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

// The dashboard's side of the deckk session: it tells the session what
// changed, and carries its suggestions into the other sessions' prompts.
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
	given  map[string]bool
	goals  string
	recent []hq.Event
	notes  map[string]hq.Note

	states   map[string]deck.State
	prs      map[string]string
	seeded   bool
	pending  []hq.Event
	told     map[string]bool
	nudgedAt time.Time
	inflight map[string]bool

	started, starting bool
	// briefed is set once this dashboard has asked for, or skipped, the
	// briefing on opening.
	briefed bool
}

func newDeckkState() deckkState {
	return deckkState{
		suggestions: map[string]hq.Suggestion{}, states: map[string]deck.State{}, prs: map[string]string{},
		told: map[string]bool{}, inflight: map[string]bool{}, given: map[string]bool{},
	}
}

type deckkSnapshot struct {
	suggestions map[string]hq.Suggestion
	questions   []hq.Question
	proposals   []hq.Proposal
	goals       string
	recent      []hq.Event
	notes       map[string]hq.Note
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
	m.deckk.proposals = snap.proposals
	m.deckk.goals, m.deckk.recent, m.deckk.notes = snap.goals, snap.recent, snap.notes
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
	return events
}

// report writes events as they happen, in order, for read_inbox, and queues
// them for the next nudge while the deckk session is listening.
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

// A delivery is text deckk types into a prompt. Every one goes through
// deliver and delivered, so none can be lost or typed twice.
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
	// news tells the deckk session what changed.
	news deliveryKind = iota
	// briefing asks the deckk session to brief the user on opening.
	briefing
	// suggestion types the deckk session's next prompt into a session, or
	// takes it back out when text is empty.
	suggestion
	// opening types the first prompt into a session that just started.
	opening
	// typed is a prompt the user wrote on the web page for a session.
	typed
	// chat is a message the user wrote on the web page for the deckk
	// session.
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

// A briefing on opening deckk is worth a turn after a real break, not after
// every relaunch.
const briefEvery = 3 * time.Hour

// nudge tells the deckk session there is news, when it is idle and nobody is
// typing to it. It reads the details itself with read_inbox. The first nudge
// after a break asks it to brief the user instead. With updates off, only
// sessions waiting on an answer get through, since they are blocked.
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

// note is what the deckk session last wrote about a session, and whether
// the session has moved on since.
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

// suggestion is the deckk session's current proposal for a session: the
// next prompt for a waiting session, made since it last changed state; a
// prompt to resume a stopped one with, made since it stopped; or a proposed
// new session's first prompt. Dismissed ones don't count.
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

// typedIn is what deckk left in a session's prompt this turn, if anything.
// Once the session takes another turn the text was sent or is the user's.
func (m Model) typedIn(s claude.Session) (store.Prefill, bool) {
	p, ok := m.store.Prefilled[s.ID]
	return p, ok && deck.StateOf(s) == deck.YourTurn && !s.StatusAt.After(p.At)
}

// prefill keeps each prompt nobody is looking at in step with the deckk
// session: it types new suggestions in, replaces ones it typed earlier, and
// takes back ones that were withdrawn or dismissed.
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

// maySend is whether the deckk session's safe suggestions for a session go
// out without review: everywhere with the send setting, or for sessions the
// user trusts.
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

// openWithSuggestion fills in a waiting suggestion on the way into a session,
// since opening it is the user choosing to see it. A proposed or stopped
// session starts first and gets its prompt once Claude is up.
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

// nextUp is the section at the top of the list: every session with a
// suggestion, newest suggestion first.
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

// faded is when the dashboard draws itself in grey: while the deckk session
// beside it is the active pane. A blur alone doesn't count, since switching
// to another app blurs the dashboard too.
func (m Model) faded() bool {
	return m.deckk.open && m.deckk.pane.Active
}

// focused is the dashboard getting the keyboard back, by a focus event or a
// click, ahead of the next refresh.
func (m *Model) focused() {
	m.deckk.pane.Active = false
}

type focusCheckMsg struct {
	pane tmux.Pane
	open bool
}

// checkFocus asks tmux where the keyboard went when the dashboard loses it,
// so fading doesn't wait for the next refresh.
func (m Model) checkFocus() tea.Cmd {
	return func() tea.Msg {
		pane, open, err := m.host.HQPane()
		if err != nil {
			return nil
		}
		return focusCheckMsg{pane, open}
	}
}

// heldBack says why something from the page wasn't typed in.
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

// toggleTrust lets the deckk session send its safe suggestions to one
// session without review, or takes that back.
func (m *Model) toggleTrust(s claude.Session) {
	m.mark(m.store.ToggleTrust(s.ID))
	if m.store.Trusted[s.ID] {
		m.setFlash("deckk may now send its safe suggestions to "+displayTitle(s)+" without asking. t again to stop", nil)
	} else {
		m.setFlash("deckk's suggestions for "+displayTitle(s)+" wait for you again", nil)
	}
}
