package ui

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/deck"
	"github.com/theowecker/deckk/internal/hq"
	"github.com/theowecker/deckk/internal/store"
	"github.com/theowecker/deckk/internal/tmux"
)

type deckkState struct {
	session     claude.Session
	running     bool
	pane        tmux.Pane
	open        bool
	suggestions map[string]hq.Suggestion
	questions   []hq.Question
	proposals   []hq.Proposal
	awaiting    []awaitingPrompt
	// given are sessions that already got a queued prompt.
	given   map[string]bool
	goals   string
	recent  []hq.Event
	notes   map[string]hq.Note
	reviews map[string]hq.Review
	links   map[string]string

	states   map[string]deck.State
	prs      map[string]string
	seeded   bool
	pending  []hq.Event
	told     map[string]bool
	nudgedAt time.Time
	inflight map[string]bool

	started, starting bool
	briefed           bool
}

func newDeckkState() deckkState {
	return deckkState{
		suggestions: map[string]hq.Suggestion{}, states: map[string]deck.State{}, prs: map[string]string{},
		told: map[string]bool{}, inflight: map[string]bool{}, given: map[string]bool{}, reviews: map[string]hq.Review{}, links: map[string]string{},
	}
}

type deckkSnapshot struct {
	suggestions map[string]hq.Suggestion
	questions   []hq.Question
	proposals   []hq.Proposal
	goals       string
	recent      []hq.Event
	notes       map[string]hq.Note
	reviews     map[string]hq.Review
	links       map[string]string
	pane        tmux.Pane
	open        bool
}

func (m Model) snapshot() deckkSnapshot {
	var snap deckkSnapshot
	snap.suggestions, _ = hq.LoadSuggestions()
	snap.questions, _ = hq.Questions()
	snap.proposals, _ = hq.LoadProposals()
	snap.goals, _ = hq.ReadGoals()
	snap.recent, _ = hq.Recent(15)
	snap.notes, _ = hq.LoadNotes()
	snap.reviews, _ = hq.LoadReviews()
	snap.links, _ = hq.LoadLinks()
	snap.pane, snap.open, _ = m.host.HQPane()
	return snap
}

// Updates are throttled so a burst of finished sessions costs one turn, but
// a session blocked on a question shouldn't wait long.
const (
	nudgeEvery    = 90 * time.Second
	questionEvery = 15 * time.Second
	// Past this the oldest news is dropped; read_inbox still has it.
	maxPending = 30
)

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
	m.deckk.proposals = snap.proposals
	m.deckk.goals, m.deckk.recent, m.deckk.notes = snap.goals, snap.recent, snap.notes
	if snap.links != nil {
		m.deckk.links = snap.links
	}
	for id, r := range snap.reviews {
		if r.At.After(m.deckk.reviews[id].At) {
			m.deckk.reviews[id] = r
		}
	}
	m.deckk.pane, m.deckk.open = snap.pane, snap.open
	return sessions
}

// The first refresh only learns the current state, so opening deckk isn't news.
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
	return events
}

func (m *Model) report(events []hq.Event) {
	if len(events) == 0 || !m.cfg.HQOn() {
		return
	}
	if err := hq.AppendEvents(events); err != nil {
		m.setFlash("", err)
	}
	if d := &m.deckk; m.cfg.Nudge() && d.open {
		d.pending = append(d.pending, events...)
		d.pending = d.pending[max(len(d.pending)-maxPending, 0):]
	}
}

// Every delivery goes through deliver and delivered, so none is lost or typed
// twice.
type delivery struct {
	kind deliveryKind
	// key is what the delivery is for within its kind: a session ID, or
	// the session a queued prompt waits on.
	key     string
	target  string
	text    string
	replace string
	send    bool
	// at is when the suggestion was made, or for news, the newest event it
	// covers.
	at        time.Time
	questions []string
}

type deliveryKind int

const (
	news deliveryKind = iota
	briefing
	suggestion
	opening
	typed
	chat
)

// slot is what can only have one delivery in flight at a time: the deckk
// session's prompt, or one session's.
func (d delivery) slot() string {
	if d.kind == news || d.kind == briefing || d.kind == chat {
		return "deckk"
	}
	return strconv.Itoa(int(d.kind)) + ":" + d.key
}

type deliveredMsg struct {
	delivery
	result tmux.Delivery
	err    error
}

func (m *Model) deliver(d delivery) tea.Cmd {
	if m.deckk.inflight[d.slot()] {
		return nil
	}
	m.deckk.inflight[d.slot()] = true
	return func() tea.Msg {
		result, err := m.host.Deliver(d.target, d.text, d.replace, d.send)
		return deliveredMsg{d, result, err}
	}
}

func (m *Model) delivered(msg deliveredMsg) {
	d := &m.deckk
	delete(d.inflight, msg.slot())
	if msg.err != nil {
		m.setFlash("", msg.err)
		return
	}
	if msg.result != tmux.Delivered {
		if msg.kind == typed || msg.kind == chat {
			m.setFlash(heldBack(msg.result), nil)
		}
		return
	}
	switch msg.kind {
	case opening:
		m.filled(msg.key)
	case briefing:
		d.nudgedAt, d.briefed = time.Now(), true
		m.mark(m.store.SetBriefed(d.nudgedAt))
	case news:
		d.nudgedAt = time.Now()
		d.pending = slices.DeleteFunc(d.pending, func(e hq.Event) bool { return !e.At.After(msg.at) })
		for _, id := range msg.questions {
			d.told[id] = true
		}
	case typed:
		if msg.replace != "" {
			m.mark(m.store.ForgetPrefill(msg.key))
			m.mark(m.store.Dismiss(msg.key, msg.at))
		}
		m.setFlash("Typed into the prompt", nil)
		if msg.send {
			m.setFlash("Sent", nil)
		}
	case chat:
		m.setFlash("Sent to the deckk session", nil)
	case suggestion:
		if msg.text == "" {
			m.mark(m.store.ForgetPrefill(msg.key))
		} else {
			m.mark(m.store.MarkPrefilled(msg.key, store.Prefill{At: msg.at, Text: msg.text}))
		}
	}
}

// A briefing is worth a turn after a real break, not after every relaunch.
const briefEvery = 3 * time.Hour

// With updates off, only sessions waiting on an answer get through, since they
// are blocked.
func (m *Model) nudge() tea.Cmd {
	d := &m.deckk
	if !m.cfg.HQOn() || !d.open || !d.running || d.pane.Seen || deck.StateOf(d.session) != deck.YourTurn {
		return nil
	}
	updates := m.cfg.Nudge()
	if updates && !d.briefed {
		if time.Since(m.store.Briefed) < briefEvery {
			d.briefed = true
		} else {
			return m.deliver(delivery{kind: briefing, target: d.pane.ID, send: true, text: m.briefing()})
		}
	}
	var asked, lines []string
	for _, q := range d.questions {
		if d.told[q.ID] || !q.Wait && !updates {
			continue
		}
		verb := "notes"
		if q.Wait {
			verb = "asks"
		}
		asked = append(asked, q.ID)
		lines = append(lines, q.Title+" "+verb+": "+clipText(q.Text, 120))
	}
	var pending []hq.Event
	if updates {
		pending = d.pending
	}
	every := nudgeEvery
	if len(asked) > 0 {
		every = questionEvery
	}
	if len(pending)+len(asked) == 0 || time.Since(d.nudgedAt) < every {
		return nil
	}
	var newest time.Time
	for _, e := range pending {
		lines = append(lines, e.Title+" "+e.Kind)
		if e.At.After(newest) {
			newest = e.At
		}
	}
	return m.deliver(delivery{
		kind: news, target: d.pane.ID, send: true,
		text: "[deckk] " + clipText(strings.Join(lines, "; "), 400) + ". Check read_inbox.",
		at:   newest, questions: asked,
	})
}

func (m Model) note(s claude.Session) (note hq.Note, ok, stale bool) {
	note, ok = m.deckk.notes[s.ID]
	if !ok || !m.cfg.HQOn() {
		return hq.Note{}, false, false
	}
	return note, true, s.UpdatedAt.After(note.At.Add(time.Minute))
}

func (m Model) briefing() string {
	waiting := 0
	for _, s := range m.sessions {
		if deck.StateOf(s) == deck.YourTurn && !s.Background {
			waiting++
		}
	}
	facts := []string{sessions(waiting) + " waiting on the user"}
	if closed := len(deck.ToRestore(m.sessions, m.marks())); closed > 0 {
		facts = append(facts, sessions(closed)+" closed since last time")
	}
	if n := len(m.left); n > 0 {
		facts = append(facts, plural(n, "checkout")+" with work left behind: "+m.left[0].line(time.Now()))
	}
	switch clashes := len(m.clashBanner()); clashes {
	case 0:
	case 1:
		facts = append(facts, "1 clash over the same files")
	default:
		facts = append(facts, strconv.Itoa(clashes)+" clashes over the same files")
	}
	return "[deckk] Briefing: the user just opened deckk. " + strings.Join(facts, ", ") +
		". Give them a short briefing and suggest or propose where the next step is clear."
}

// A waiting session's suggestion counts only if made since it last changed
// state, a stopped one's only if made since it stopped.
func (m Model) suggestion(s claude.Session) (hq.Suggestion, bool) {
	if !m.cfg.HQOn() {
		return hq.Suggestion{}, false
	}
	sg, ok := m.deckk.suggestions[s.ID]
	if p, proposed := m.proposal(s); proposed {
		sg, ok = hq.Suggestion{Prompt: p.Prompt, Why: p.Why, At: p.At}, true
	}
	switch state := deck.StateOf(s); {
	case !ok:
		return hq.Suggestion{}, false
	case isProposal(s):
	case state == deck.YourTurn && !sg.At.Before(s.StatusAt):
	case state == deck.Stopped && sg.At.After(s.UpdatedAt):
	default:
		return hq.Suggestion{}, false
	}
	if at, dismissed := m.store.Dismissed[s.ID]; dismissed && at.Equal(sg.At) {
		return hq.Suggestion{}, false
	}
	return sg, true
}

func (m Model) prefilled(s claude.Session, sg hq.Suggestion) bool {
	return m.store.Prefilled[s.ID].At.Equal(sg.At)
}

// Once the session takes another turn, the text was sent or is the user's.
func (m Model) typedIn(s claude.Session) (store.Prefill, bool) {
	p, ok := m.store.Prefilled[s.ID]
	return p, ok && deck.StateOf(s) == deck.YourTurn && !s.StatusAt.After(p.At)
}

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
		id, hosted := m.window(s)
		if !hosted || active[id] {
			continue
		}
		cmds = append(cmds, m.syncPrompt(s, m.maySend(s)))
	}
	return tea.Batch(cmds...)
}

func (m Model) maySend(s claude.Session) bool {
	return m.cfg.HQOn() && (m.cfg.Send() || m.store.Trusted[s.ID])
}

func (m *Model) syncPrompt(s claude.Session, maySend bool) tea.Cmd {
	sg, suggested := m.suggestion(s)
	prev, typed := m.typedIn(s)
	switch {
	case suggested && !m.prefilled(s, sg):
		return m.deliver(delivery{kind: suggestion, key: s.ID, target: s.TTY, text: sg.Prompt, replace: prev.Text, send: maySend && sg.Safe, at: sg.At})
	case !suggested && typed:
		return m.deliver(delivery{kind: suggestion, key: s.ID, target: s.TTY, replace: prev.Text})
	}
	if _, kept := m.store.Prefilled[s.ID]; kept && !typed {
		m.mark(m.store.ForgetPrefill(s.ID))
	}
	return nil
}

func (m *Model) openWithSuggestion(s claude.Session) tea.Cmd {
	if sg, ok := m.suggestion(s); ok && (isProposal(s) || !s.Running()) {
		return m.begin(s, sg)
	}
	if _, hosted := m.window(s); !hosted || !m.cfg.Prefill() {
		return m.open(s)
	}
	return tea.Sequence(m.syncPrompt(s, false), m.open(s))
}

func (m *Model) dismiss(s claude.Session) tea.Cmd {
	sg, ok := m.suggestion(s)
	if !ok {
		return nil
	}
	m.mark(m.store.Dismiss(s.ID, sg.At))
	if p, ok := m.proposal(s); ok {
		return withdraw(p.ID)
	}
	if _, hosted := m.window(s); hosted && m.cfg.Prefill() {
		return m.syncPrompt(s, false)
	}
	return nil
}

func (m Model) nextUp() (deck.Group, bool) {
	list := m.nextUpSessions()
	if m.view != sessionsView || m.byProject || len(list) == 0 {
		return deck.Group{}, false
	}
	return deck.Group{Section: deck.NextUp, Title: deck.NextUp.String(), Sessions: list}, true
}

func (m Model) nextUpSessions() []claude.Session {
	list := m.openProposals()
	for _, s := range m.sessions {
		if _, ok := m.suggestion(s); ok && !s.Background {
			list = append(list, s)
		}
	}
	at := func(s claude.Session) time.Time {
		sg, _ := m.suggestion(s)
		return sg.At
	}
	slices.SortFunc(list, func(a, b claude.Session) int { return at(b).Compare(at(a)) })
	return list
}

func (m *Model) openDeckk() tea.Cmd {
	if !m.cfg.HQOn() {
		m.setFlash("The deckk session is off. Turn it on in settings, s", nil)
		return nil
	}
	return m.do(func() error { return m.host.OpenHQ(*m.cfg, true) }, "")
}

type deckkStartedMsg struct{ err error }

// Never reopened behind the user's back, so closing it sticks until c or the
// next launch.
func (m *Model) startDeckk() tea.Cmd {
	if m.deckk.started || m.deckk.starting || !m.cfg.HQOn() {
		return nil
	}
	m.deckk.starting = true
	return func() tea.Msg { return deckkStartedMsg{m.host.OpenHQ(*m.cfg, false)} }
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

// A blur alone doesn't count, since switching to another app blurs the
// dashboard too.
func (m Model) faded() bool {
	return m.deckk.open && m.deckk.pane.Active
}

func (m *Model) focused() {
	m.deckk.pane.Active = false
}

type focusCheckMsg struct {
	pane tmux.Pane
	open bool
}

func (m Model) checkFocus() tea.Cmd {
	return func() tea.Msg {
		pane, open, err := m.host.HQPane()
		if err != nil {
			return nil
		}
		return focusCheckMsg{pane, open}
	}
}

func heldBack(result tmux.Delivery) string {
	switch result {
	case tmux.Busy:
		return "That prompt already has text in it, so nothing was typed. Send or clear it first"
	case tmux.Scrolled:
		return "That pane is scrolled back. Scroll to the bottom and try again"
	default:
		return "Claude is asking something there, so nothing was typed. Answer it first"
	}
}

func (m *Model) toggleTrust(s claude.Session) {
	m.mark(m.store.ToggleTrust(s.ID))
	if m.store.Trusted[s.ID] {
		m.setFlash("deckk may now send its safe suggestions to "+displayTitle(s)+" without asking. t again to stop", nil)
	} else {
		m.setFlash("deckk's suggestions for "+displayTitle(s)+" wait for you again", nil)
	}
}

// deckkLine is the deckk session's latest word, the headline its briefs open
// with, so it can be read at a glance whatever its pane shows.
func (m Model) deckkLine() string {
	d := m.deckk
	if !m.cfg.HQOn() || !d.running || m.picker != nil || m.settings {
		return ""
	}
	if deck.StateOf(d.session) == deck.Working {
		return pad() + accent.Render("◆ ") + ghost.Render("deckk is thinking…")
	}
	headline := firstLine(d.session.LastReply)
	if headline == "" {
		return ""
	}
	return pad() + accent.Render("◆ deckk  ") + soft.Render(headline)
}

func firstLine(text string) string {
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(strings.NewReplacer("**", "", "__", "", "`", "").Replace(line))
		line = strings.TrimLeft(line, "#>-* ")
		if line != "" {
			return line
		}
	}
	return ""
}
