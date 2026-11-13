package ui

import (
	"errors"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/deck"
	"github.com/theowecker/deckk/internal/hq"
	"github.com/theowecker/deckk/internal/tmux"
)

// The dashboard's side of the deckk session: it tells the session what
// changed, and carries its suggestions into the other sessions' prompts.
type deckkState struct {
	session     claude.Session
	running     bool
	pane        tmux.Pane
	open        bool
	suggestions map[string]hq.Suggestion
	questions   []hq.Question

	states   map[string]deck.State
	prs      map[string]string
	seeded   bool
	pending  []hq.Event
	told     map[string]bool
	nudgedAt time.Time
	inflight map[string]bool

	started, starting bool
}

func newDeckkState() deckkState {
	return deckkState{
		suggestions: map[string]hq.Suggestion{}, states: map[string]deck.State{}, prs: map[string]string{},
		told: map[string]bool{}, inflight: map[string]bool{},
	}
}

type deckkSnapshot struct {
	suggestions map[string]hq.Suggestion
	questions   []hq.Question
	pane        tmux.Pane
	open        bool
}

func (m Model) snapshot() deckkSnapshot {
	var snap deckkSnapshot
	snap.suggestions, _ = hq.LoadSuggestions()
	snap.questions, _ = hq.Questions()
	snap.pane, snap.open, _ = m.host.HQPane()
	return snap
}

// Updates are throttled so a burst of finished sessions costs one turn, but
// a session blocked on a question shouldn't wait long.
const (
	nudgeEvery    = 90 * time.Second
	questionEvery = 15 * time.Second
)

// takeDeckk pulls the deckk session out of the list, since it isn't one of
// the user's sessions, and records what the files and pane say.
func (m *Model) takeDeckk(sessions []claude.Session, snap deckkSnapshot) []claude.Session {
	m.deckk.running = false
	sessions = slices.DeleteFunc(sessions, func(s claude.Session) bool {
		if !hq.Is(s.Cwd) {
			return false
		}
		if s.Running() {
			m.deckk.session, m.deckk.running = s, true
		}
		return true
	})
	if snap.suggestions != nil {
		m.deckk.suggestions = snap.suggestions
	}
	m.deckk.questions = snap.questions
	m.deckk.pane, m.deckk.open = snap.pane, snap.open
	return sessions
}

// trackEvents notes what changed since the last refresh. The first refresh
// only learns the current state, so opening deckk isn't news.
func (m *Model) trackEvents() []hq.Event {
	d := &m.deckk
	now := time.Now()
	var events []hq.Event
	for _, s := range m.sessions {
		if s.Background {
			continue
		}
		state := deck.StateOf(s)
		prev, known := d.states[s.ID]
		if d.seeded && known {
			switch {
			case prev == deck.Working && state == deck.YourTurn:
				events = append(events, hq.Event{At: now, Session: s.ID, Title: displayTitle(s), Kind: hq.Finished, Detail: clipText(s.LastReply, 160)})
			case prev != deck.Stopped && state == deck.Stopped:
				events = append(events, hq.Event{At: now, Session: s.ID, Title: displayTitle(s), Kind: hq.Closed})
			}
			if d.prs[s.ID] == "" && s.PR != "" {
				events = append(events, hq.Event{At: now, Session: s.ID, Title: displayTitle(s), Kind: hq.OpenedPR, Detail: s.PR})
			}
		}
		if s.Running() || known {
			d.states[s.ID] = state
			d.prs[s.ID] = s.PR
		}
	}
	d.seeded = true
	d.pending = append(d.pending, events...)
	return events
}

func (m Model) recordEvents(events []hq.Event) tea.Cmd {
	if len(events) == 0 || !m.cfg.HQOn() {
		return nil
	}
	return func() tea.Msg {
		if err := hq.AppendEvents(events); err != nil {
			return flashMsg{err: err}
		}
		return nil
	}
}

type nudgedMsg struct {
	sent      bool
	events    int
	questions []string
}

// nudge tells the deckk session there is news, when it is idle and nobody is
// typing to it. It reads the details itself with read_inbox.
func (m *Model) nudge() tea.Cmd {
	d := &m.deckk
	if !m.cfg.Nudge() || !d.open || !d.running || d.pane.Seen || deck.StateOf(d.session) != deck.YourTurn {
		return nil
	}
	var asked []string
	var lines []string
	for _, q := range d.questions {
		if !d.told[q.ID] {
			asked = append(asked, q.ID)
			lines = append(lines, q.Title+" "+map[bool]string{true: "asks", false: "notes"}[q.Wait]+": "+clipText(q.Text, 120))
		}
	}
	every := nudgeEvery
	if len(asked) > 0 {
		every = questionEvery
	}
	if len(d.pending)+len(asked) == 0 || time.Since(d.nudgedAt) < every {
		return nil
	}
	for _, e := range d.pending {
		lines = append(lines, e.Title+" "+e.Kind)
	}
	text := "[deckk] " + clipText(strings.Join(lines, "; "), 400) + ". Check read_inbox."
	d.nudgedAt = time.Now()
	events, pane := len(d.pending), d.pane.ID
	return func() tea.Msg {
		sent, err := m.host.Deliver(pane, text, true)
		if err != nil {
			return flashMsg{err: err}
		}
		return nudgedMsg{sent: sent, events: events, questions: asked}
	}
}

func (m *Model) nudged(msg nudgedMsg) {
	if !msg.sent {
		return
	}
	m.deckk.pending = m.deckk.pending[min(msg.events, len(m.deckk.pending)):]
	for _, id := range msg.questions {
		m.deckk.told[id] = true
	}
}

// suggestion is the deckk session's current proposal for a session: made
// since the session last changed state, while it waits, and not dismissed.
func (m Model) suggestion(s claude.Session) (hq.Suggestion, bool) {
	sg, ok := m.deckk.suggestions[s.ID]
	if !ok || !m.cfg.HQOn() || deck.StateOf(s) != deck.YourTurn || sg.At.Before(s.StatusAt) {
		return hq.Suggestion{}, false
	}
	if at, dismissed := m.store.Dismissed[s.ID]; dismissed && at.Equal(sg.At) {
		return hq.Suggestion{}, false
	}
	return sg, true
}

func (m Model) prefilled(s claude.Session, sg hq.Suggestion) bool {
	at, ok := m.store.Prefilled[s.ID]
	return ok && at.Equal(sg.At)
}

type prefilledMsg struct {
	id   string
	at   time.Time
	done bool
}

// prefill types waiting suggestions into sessions nobody is looking at, so
// the next prompt is ready when the user switches over.
func (m *Model) prefill() tea.Cmd {
	if !m.cfg.Prefill() {
		return nil
	}
	active := map[string]bool{}
	for _, w := range m.tabs {
		active[w.ID] = w.Active
	}
	var cmds []tea.Cmd
	for _, s := range m.sessions {
		sg, ok := m.suggestion(s)
		id, hosted := m.window(s)
		if !ok || !hosted || active[id] || m.prefilled(s, sg) || m.deckk.inflight[s.ID] {
			continue
		}
		cmds = append(cmds, m.deliver(s, id, sg, m.cfg.Send() && sg.Safe))
	}
	return tea.Batch(cmds...)
}

func (m *Model) deliver(s claude.Session, target string, sg hq.Suggestion, send bool) tea.Cmd {
	m.deckk.inflight[s.ID] = true
	id := s.ID
	return func() tea.Msg {
		done, err := m.host.Deliver(target, sg.Prompt, send)
		if err != nil {
			return flashMsg{err: err}
		}
		return prefilledMsg{id: id, at: sg.At, done: done}
	}
}

func (m *Model) donePrefill(msg prefilledMsg) {
	delete(m.deckk.inflight, msg.id)
	if msg.done {
		m.mark(m.store.MarkPrefilled(msg.id, msg.at))
	}
}

// openWithSuggestion fills in a waiting suggestion on the way into a session,
// since opening it is the user choosing to see it.
func (m *Model) openWithSuggestion(s claude.Session) tea.Cmd {
	sg, ok := m.suggestion(s)
	id, hosted := m.window(s)
	if !ok || !hosted || !m.cfg.Prefill() || m.prefilled(s, sg) || m.deckk.inflight[s.ID] {
		return m.open(s)
	}
	return tea.Sequence(m.deliver(s, id, sg, false), m.open(s))
}

func (m *Model) dismiss(s claude.Session) {
	if sg, ok := m.suggestion(s); ok {
		m.mark(m.store.Dismiss(s.ID, sg.At))
	}
}

// nextUp is the section at the top of the list: every session with a
// suggestion, newest suggestion first.
func (m Model) nextUp() (deck.Group, bool) {
	if m.view != sessionsView || m.byProject {
		return deck.Group{}, false
	}
	var list []claude.Session
	for _, s := range m.inView(m.sessions) {
		if _, ok := m.suggestion(s); ok {
			list = append(list, s)
		}
	}
	if len(list) == 0 {
		return deck.Group{}, false
	}
	slices.SortFunc(list, func(a, b claude.Session) int {
		return m.deckk.suggestions[b.ID].At.Compare(m.deckk.suggestions[a.ID].At)
	})
	return deck.Group{Section: deck.NextUp, Title: deck.NextUp.String(), Sessions: list}, true
}

func (m Model) openDeckk() tea.Cmd {
	return m.do(func() error { return m.host.OpenHQ(true) }, "")
}

type deckkStartedMsg struct{ err error }

// startDeckk opens the deckk session once per dashboard, as soon as a client
// is attached. Once started it is never reopened behind the user's back, so
// closing it sticks until c or the next launch.
func (m *Model) startDeckk() tea.Cmd {
	if m.deckk.started || m.deckk.starting || !m.cfg.HQOn() {
		return nil
	}
	m.deckk.starting = true
	return func() tea.Msg { return deckkStartedMsg{m.host.OpenHQ(false)} }
}

func (m *Model) deckkStarted(msg deckkStartedMsg) {
	m.deckk.starting = false
	switch {
	case errors.Is(msg.err, tmux.ErrDetached):
	case msg.err != nil:
		m.deckk.started = true
		m.setFlash("", msg.err)
	default:
		m.deckk.started = true
	}
}

func clipText(s string, n int) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}
