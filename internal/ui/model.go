package ui

import (
	"maps"
	"os"
	"slices"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/config"
	"github.com/theowecker/deckk/internal/deck"
	"github.com/theowecker/deckk/internal/git"
	"github.com/theowecker/deckk/internal/github"
	"github.com/theowecker/deckk/internal/store"
	"github.com/theowecker/deckk/internal/tmux"
)

const refreshEvery = 2 * time.Second

type Host interface {
	Windows() ([]tmux.Window, error)
	Open(dir, name, command string, focus bool) error
	Select(windowID string) error
	Close(windowID string) error
	SetLabels(labels map[string]tmux.Label) error
	Detach() error
	Focus(tty string) error
	Release(pid int, tty string) error
	PopOut(windowID string) error
	BringBack(windowID string) error
	MoveAfter(windowID, afterID string) error
	SetTabKeys(preset string) error
	SetRestore(n int) error
	LayoutHQ(show, expanded bool) error

	OpenHQ(cfg config.Config, focus bool) error
	CloseHQ() error
	HQPane() (tmux.Pane, bool, error)
	Notify(id, title, subtitle, body string) error
	Deliver(target, text, replace string, send bool) (tmux.Delivery, error)
}

type Model struct {
	home  string
	since time.Time
	store *store.Store
	cfg   *config.Config
	host  Host

	sessions []claude.Session
	windows  map[string]string
	popped   map[string]tmux.Window
	tabs     []tmux.Window
	labels   map[string]tmux.Label
	groups   []deck.Group
	flat     []claude.Session
	loaded   bool
	folders  []string

	cursor       int
	selectedID   string
	offset       int
	view         view
	showAll      bool
	everything   []claude.Session
	mentioned    mentionedMsg
	byProject    bool
	filter       textinput.Model
	picker       *picker
	settings     bool
	setting      int
	renaming     *textinput.Model
	renameID     string
	help         bool
	confirm      string
	seen         map[string]bool
	wasRunning   map[string]bool
	trackedOnce  bool
	closing      map[string]bool
	resuming     map[string]time.Time
	reviewing    map[string]bool
	reviewQueue  []string
	reviewOpen   string
	prSeen       map[string]github.PR
	leftAt       time.Time
	restoreShown int
	layoutShown  hqLayout
	left         []leftBehind
	lastSeen     map[string]time.Time
	deckk        deckkState
	collisions   []deck.Collision
	clashes      map[string]map[string]bool
	clashesSeen  bool
	web          Publisher
	notified     map[string]time.Time
	flashSeq     int
	webStart     WebStarter
	typingGap    time.Duration
	lastLetter   time.Time
	lastRune     rune
	inWord       bool

	lastClick  time.Time
	lastClickY int
	pointer    string
	loadedAt   time.Time
	settingAt  time.Time
	startedAt  time.Time

	flash    string
	flashErr bool
	frame    int
	width    int
	height   int
}

func New(home string, since time.Time, st *store.Store, cfg *config.Config, host Host) Model {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.Placeholder = "title, project, branch, prompt or reply"
	ti.PromptStyle = accent
	ti.Cursor.Style = accent
	return Model{
		home: home, since: since, store: st, cfg: cfg, host: host, filter: ti,
		labels: map[string]tmux.Label{}, seen: map[string]bool{}, startedAt: time.Now(),
		wasRunning: map[string]bool{}, closing: map[string]bool{}, resuming: map[string]time.Time{}, reviewing: map[string]bool{}, lastSeen: map[string]time.Time{},
		typingGap: typingGap, notified: map[string]time.Time{}, restoreShown: -1,
		deckk: newDeckkState(), clashes: map[string]map[string]bool{},
	}
}

type loadedMsg struct {
	sessions  []claude.Session
	windows   []tmux.Window
	err       error
	scheduled bool
	started   time.Time
	deckk     deckkSnapshot
	edits     map[string][]string
	// cfg is the config file again when something else changed it, such as
	// the deckk session's intro turning the session off.
	cfg *config.Config
	// Read with the sessions, off the UI goroutine, because finding them lists
	// directories.
	folders []string
	prs     map[string]github.PR
}

type labelsSetMsg map[string]tmux.Label

type tickMsg struct{}
type spinMsg struct{}

type flashMsg struct {
	text string
	err  error
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.load(true), spin())
}

// Only scheduled loads re-arm the timer, or polling loops would stack up.
func (m Model) load(scheduled bool) tea.Cmd {
	started := time.Now()
	pinned := maps.Clone(m.store.Pinned)
	web := m.web != nil
	return func() tea.Msg {
		sessions, err := claude.Load(m.home, m.since, pinned)
		if err != nil {
			return loadedMsg{err: err, scheduled: scheduled, started: started}
		}
		windows, err := m.host.Windows()
		msg := loadedMsg{sessions, windows, err, scheduled, started, m.snapshot(), claude.EditsOf(sessions), m.changedConfig(), nil, nil}
		if web {
			msg.folders = candidateDirs(sessions)
		}
		msg.prs = github.Statuses(prURLs(sessions, started))
		return msg
	}
}

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

func spin() tea.Cmd {
	return tea.Tick(90*time.Millisecond, func(time.Time) tea.Msg { return spinMsg{} })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scroll()
		return m, m.syncLabels()

	case loadedMsg:
		var cmd tea.Cmd
		switch {
		case msg.started.Before(m.loadedAt):
			// An older refresh finished after a newer one, so its data is stale.
		case msg.err != nil:
			m.setFlash("", msg.err)
		default:
			if m.flashErr {
				m.flash, m.flashErr = "", false
			}
			m.loadedAt = msg.started
			// A setting changed here after the load read the file is newer.
			if msg.cfg != nil && !m.settingAt.After(msg.started) {
				m.cfg = msg.cfg
			}
			m.sessions = m.takeDeckk(msg.sessions, msg.deckk)
			for i, s := range m.sessions {
				m.sessions[i].PRStatus = msg.prs[s.PRURL]
			}
			m.store.ApplyNames(m.sessions)
			m.windows, m.popped = byTTY(msg.windows)
			for _, s := range msg.sessions {
				if s.Running() {
					delete(m.resuming, s.ID)
				}
			}
			if msg.folders != nil {
				m.folders = msg.folders
			}
			m.tabs = msg.windows
			m.loaded = true
			m.trackWorkspace()
			m.trackClosed()
			m.trackSeen()
			m.collisions = deck.Collisions(m.sessions, msg.edits, git.Root)
			events := append(m.trackEvents(), m.trackPRs()...)
			m.report(append(events, m.trackCollisions()...))
			m.rearrange()
			cmd = tea.Batch(m.syncLayout(), m.syncRestore(), m.syncLabels(), m.nudge(), m.prefill(), m.fillStarted(), m.startDeckk(), m.notify(events), m.autoReview(events), m.findLeftBehind(time.Now()))
			m.publish()
		}
		if msg.scheduled {
			return m, tea.Batch(cmd, tick())
		}
		return m, cmd

	case labelsSetMsg:
		for id, label := range msg {
			m.labels[id] = label
		}
		return m, nil

	case tickMsg:
		return m, m.load(true)

	case spinMsg:
		m.frame++
		if m.frame%reelEvery == 0 {
			return m, tea.Batch(spin(), m.syncLabels())
		}
		return m, spin()

	case deckkStartedMsg:
		m.deckkStarted(msg)
		return m, nil

	case webActionMsg:
		cmd := m.webAction(msg)
		m.publish()
		return m, cmd

	case deliveredMsg:
		m.delivered(msg)
		m.publish()
		return m, nil

	case pinTabMsg:
		cmd := m.pinTab(string(msg))
		return m, cmd

	case flashMsg:
		m.setFlash(msg.text, msg.err)
		return m, m.load(false)

	case tea.KeyMsg:
		if msg.Type == tea.KeyF9 {
			return m, m.activeTab(func(tty string) tea.Msg { return acceptMsg(tty) })
		}
		if msg.Type == tea.KeyF10 {
			return m, m.restoreWorkspace()
		}
		if msg.Type == tea.KeyF11 {
			return m, m.currentTab()
		}
		if msg.Type == tea.KeyF12 {
			m.help, m.settings, m.renaming = false, false, nil
			return m.openPicker()
		}
		switch {
		case m.help:
			m.help = false
			return m, nil
		case m.picker != nil:
			return m.updatePicker(msg)
		case m.settings:
			return m.updateSettings(msg)
		case m.renaming != nil:
			return m.updateRename(msg)
		case m.view == findView:
			return m.updateFind(msg)
		}
		return m.updateList(msg)

	case resumeFailedMsg:
		for _, id := range msg.ids {
			delete(m.resuming, id)
		}
		m.setFlash("", msg.err)
		return m, nil

	case acceptMsg:
		return m, m.acceptSuggestion(string(msg))

	case leftBehindMsg:
		m.left = msg
		m.publish()
		return m, nil

	case reviewDoneMsg:
		m.reviewed(msg)
		m.publish()
		return m, tea.Batch(m.notifyReview(msg), m.nextReview())

	case searchSoonMsg:
		if m.view == findView && string(msg) == m.filter.Value() {
			return m, m.searchTranscripts(string(msg))
		}
		return m, nil

	case mentionedMsg:
		if msg.query == m.filter.Value() {
			m.mentioned = msg
			m.rearrange()
		}
		return m, nil

	case foundMsg:
		m.everything = msg
		if p := m.picker; p != nil {
			cursor := p.cursor
			p.pool = m.pickerPool()
			p.refilter()
			p.cursor = min(cursor, max(p.rows()-1, 0))
		}
		m.rearrange()
		return m, nil

	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress {
			m.focused()
		}
		return m.updateMouse(msg)

	case tea.BlurMsg:
		m.pointer = ""
		return m, m.checkFocus()

	case focusCheckMsg:
		m.deckk.pane, m.deckk.open = msg.pane, msg.open
		return m, m.syncLayout()

	case tea.FocusMsg:
		m.focused()
		return m, m.syncLayout()
	}
	return m, nil
}

func byTTY(windows []tmux.Window) (map[string]string, map[string]tmux.Window) {
	strip := map[string]string{}
	popped := map[string]tmux.Window{}
	for _, w := range windows {
		if w.PoppedOut() {
			popped[w.TTY] = w
		} else {
			strip[w.TTY] = w.ID
		}
	}
	return strip, popped
}

func (m Model) window(s claude.Session) (string, bool) {
	if !s.Running() || s.TTY == "" {
		return "", false
	}
	id, ok := m.windows[s.TTY]
	return id, ok
}

func (m Model) poppedOut(s claude.Session) (tmux.Window, bool) {
	if !s.Running() || s.TTY == "" {
		return tmux.Window{}, false
	}
	w, ok := m.popped[s.TTY]
	return w, ok
}

func (m Model) inDeckk(s claude.Session) bool {
	_, inStrip := m.window(s)
	_, popped := m.poppedOut(s)
	return inStrip || popped
}

func (m *Model) setFlash(text string, err error) {
	m.flashErr = err != nil
	if err != nil {
		text = err.Error()
	}
	m.flash = text
	if text != "" {
		m.flashSeq++
	}
}

func (m Model) marks() deck.Marks {
	hosted := map[string]bool{}
	for _, s := range m.sessions {
		if _, ok := m.window(s); ok {
			hosted[s.ID] = true
		}
	}
	order := map[string]int{}
	byTTY := map[string]string{}
	for _, s := range m.sessions {
		if s.Running() {
			byTTY[s.TTY] = s.ID
		}
	}
	for i, w := range m.tabs {
		if id, ok := byTTY[w.TTY]; ok {
			order[id] = i
		}
	}
	return deck.Marks{
		Pinned: m.store.Pinned, Archived: m.store.Archived, Open: m.store.Open,
		Hosted: hosted, Order: order, Closed: m.store.Closed, Now: time.Now(),
	}
}

func (m *Model) trackClosed() {
	// Seeding from disk lets sessions lost in a restart count as closed.
	if !m.trackedOnce {
		m.wasRunning = maps.Clone(m.store.Running)
		m.trackedOnce = true
	}
	var closed, reopened []string
	running := map[string]bool{}
	for _, s := range m.sessions {
		if s.Running() && !s.Background {
			running[s.ID] = true
			reopened = append(reopened, s.ID)
		}
	}
	for id := range m.wasRunning {
		if !running[id] && !m.closing[id] {
			closed = append(closed, id)
		}
		delete(m.closing, id)
	}
	m.wasRunning = running
	if err := m.store.SetClosed(closed, reopened, time.Now(), deck.ClosedFor); err != nil {
		m.setFlash("", err)
	}
	if err := m.store.SetRunning(running); err != nil {
		m.setFlash("", err)
	}
}

func (m *Model) trackSeen() {
	now := time.Now()
	first := len(m.lastSeen) == 0
	active := map[string]bool{}
	for _, w := range m.tabs {
		if w.Active {
			active[w.TTY] = true
		}
	}
	for _, s := range m.sessions {
		if first || active[s.TTY] {
			m.lastSeen[s.ID] = now
		}
	}
}

func (m Model) unread(s claude.Session) bool {
	_, inStrip := m.window(s)
	seen, ok := m.lastSeen[s.ID]
	return inStrip && deck.StateOf(s) == deck.YourTurn && (!ok || s.StatusAt.After(seen))
}

// A session only leaves the workspace once this dashboard saw it close, so a
// reboot, which kills the dashboard too, never empties it.
func (m *Model) trackWorkspace() {
	var added, removed []string
	hosted := map[string]bool{}
	for _, s := range m.sessions {
		if m.inDeckk(s) {
			hosted[s.ID] = true
			added = append(added, s.ID)
			m.seen[s.ID] = true
		}
	}
	for id := range m.seen {
		if !hosted[id] {
			removed = append(removed, id)
		}
	}
	// Every session vanishing at once is tmux or the machine going down,
	// not the user closing them, so the workspace keeps them to reopen.
	if len(hosted) == 0 && len(removed) > 1 {
		removed = slices.DeleteFunc(removed, func(id string) bool { return !m.closing[id] })
	}
	for _, id := range removed {
		delete(m.seen, id)
	}
	if err := m.store.SetOpen(added, removed); err != nil {
		m.setFlash("", err)
	}
}

func (m *Model) rearrange() {
	if m.view == findView {
		m.groups = m.findResults()
	} else {
		arrange := deck.Arrange
		if m.byProject {
			arrange = deck.ArrangeByProject
		}
		switch m.view {
		case homeView:
			m.groups = m.homeGroups()
		case boardView:
			m.groups = m.board()
		default:
			m.groups = arrange(yours(m.sessions), m.marks(), "", false)
			if bg := m.background(); len(bg.Sessions) > 0 {
				m.groups = append(m.groups, bg)
			}
		}
	}
	if !m.showAll && m.view != findView {
		for i := range m.groups {
			if limit, ok := m.collapseLimit(m.groups[i]); ok {
				m.groups[i].Collapse(limit)
			}
		}
	}
	m.flat = m.flat[:0]
	for _, g := range m.groups {
		m.flat = append(m.flat, g.Sessions...)
	}
	for i, s := range m.flat {
		if s.ID == m.selectedID {
			m.cursor = i
			break
		}
	}
	m.move(0)
}

func (m Model) collapseLimit(g deck.Group) (int, bool) {
	switch {
	case m.view == boardView:
		return 3, true
	case m.byProject:
		return 5, true
	case g.Section == deck.Background:
		return backgroundShown, true
	case m.view == homeView:
		return 0, false
	case g.Section == deck.Recent || g.Section == deck.Closed || g.Section == deck.Archived:
		return 10, true
	}
	return 0, false
}

func (m *Model) move(delta int) {
	m.cursor = max(0, min(m.cursor+delta, len(m.flat)-1))
	if s, ok := m.selected(); ok {
		m.selectedID = s.ID
	}
	m.scroll()
}

func (m Model) selected() (claude.Session, bool) {
	if m.cursor < 0 || m.cursor >= len(m.flat) {
		return claude.Session{}, false
	}
	return m.flat[m.cursor], true
}

// A change made outside the dashboard isn't overwritten by its next save.
func (m Model) changedConfig() *config.Config {
	info, err := os.Stat(m.cfg.Path())
	if err != nil || !info.ModTime().After(m.loadedAt) {
		return nil
	}
	fresh, err := config.Load(m.cfg.Path())
	if err != nil {
		return nil
	}
	return fresh
}
