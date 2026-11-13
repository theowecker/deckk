package ui

import (
	"cmp"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/deck"
	"github.com/theowecker/deckk/internal/hq"
	"github.com/theowecker/deckk/internal/web"
)

type Publisher interface {
	Publish(state any)
	URL() string
	Close() error
}

type WebStarter func() (Publisher, error)

func (m Model) WithWeb(start WebStarter) Model {
	m.webStart = start
	if m.cfg.WebOn() {
		m.startWeb()
	}
	return m
}

func (m *Model) startWeb() {
	if m.web != nil || m.webStart == nil {
		return
	}
	web, err := m.webStart()
	if err != nil {
		m.setFlash("", err)
		return
	}
	m.web = web
	m.publish()
}

func (m *Model) stopWeb() {
	if m.web == nil {
		return
	}
	if err := m.web.Close(); err != nil {
		m.setFlash("", err)
	}
	m.web = nil
}

type webActionMsg web.Action

func WebAction(a web.Action) tea.Msg { return webActionMsg(a) }

func (m Model) publish() {
	if m.web != nil {
		m.web.Publish(m.webState())
	}
}

func (m *Model) webAction(a webActionMsg) tea.Cmd {
	switch a.Kind {
	case web.Talk:
		return m.openDeckk()
	case web.Chat:
		return m.chat(a.Text)
	case web.Goals:
		if err := hq.WriteGoals(a.Text); err != nil {
			m.setFlash("", err)
			return nil
		}
		m.deckk.goals = a.Text
		m.setFlash("Goals saved", nil)
		return nil
	case web.New:
		return m.newFromWeb(a.Folder, a.Text, a.Send)
	}
	s, ok := m.sessionByID(a.ID)
	if !ok {
		m.setFlash("That session is gone", nil)
		return nil
	}
	switch a.Kind {
	case web.Open:
		return m.openWithSuggestion(s)
	case web.Dismiss:
		return m.dismiss(s)
	case web.Pin:
		m.mark(m.store.TogglePin(s.ID))
	case web.Archive:
		m.mark(m.store.ToggleArchive(s.ID))
	case web.Trust:
		m.toggleTrust(s)
	case web.Review:
		return m.review(s, false)
	case web.Rename:
		m.mark(m.store.Rename(s.ID, a.Text))
		m.store.ApplyNames(m.sessions)
		m.rearrange()
		return m.syncLabels()
	case web.Prompt:
		return m.promptFromWeb(s, a.Text, a.Send)
	case web.Close:
		return m.closeFromWeb(s, a.Confirm)
	}
	return nil
}

// Leaves the terminal on what it shows, and queues the first prompt for when
// Claude is up.
func (m *Model) newFromWeb(folder, prompt string, send bool) tea.Cmd {
	dir := filepath.Clean(expandHome(strings.TrimSpace(folder)))
	if !filepath.IsAbs(dir) {
		m.setFlash("Give the folder as a full path, like ~/dev/app", nil)
		return nil
	}
	create := !isDir(dir)
	if create && !isDir(filepath.Dir(dir)) {
		m.setFlash(abbreviateHome(filepath.Dir(dir))+" doesn't exist", nil)
		return nil
	}
	now := time.Now()
	if strings.TrimSpace(prompt) != "" {
		m.deckk.awaiting = append(m.deckk.awaiting, awaitingPrompt{key: "new:" + dir + now.Format(time.RFC3339Nano), dir: dir, after: now, prompt: prompt, send: send})
	}
	m.setFlash("Starting Claude in "+abbreviateHome(dir), nil)
	return m.do(func() error {
		if create {
			if err := os.Mkdir(dir, 0o755); err != nil {
				return err
			}
		}
		return m.host.Open(dir, tabName(filepath.Base(dir)), m.claude(), false)
	}, "")
}

// The page asks first when Claude is working, so confirm stands for the second
// press of x.
func (m *Model) closeFromWeb(s claude.Session, confirm bool) tea.Cmd {
	if !m.closable(s) {
		m.setFlash(displayTitle(s)+" runs in another tab, so close it there", nil)
		return nil
	}
	if deck.StateOf(s) == deck.Working && !confirm {
		m.setFlash("Claude is still working in "+displayTitle(s)+". Close it again to stop it anyway", nil)
		return nil
	}
	next, cmd := m.close(s, "x"+s.ID)
	*m = next.(Model)
	return cmd
}

func (m Model) closable(s claude.Session) bool {
	_, hosted := m.window(s)
	_, popped := m.poppedOut(s)
	return hosted || popped
}

func (m *Model) promptFromWeb(s claude.Session, text string, send bool) tea.Cmd {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if !s.Running() {
		now := time.Now()
		m.deckk.awaiting = append(m.deckk.awaiting, awaitingPrompt{key: "web:" + s.ID + now.Format(time.RFC3339Nano), sessionID: s.ID, dir: s.Dir, after: now, prompt: text, send: send})
		m.setFlash("Resuming "+displayTitle(s)+"; your prompt goes in once Claude is ready", nil)
		if !m.claimResume(s.ID) {
			return nil
		}
		return m.do(func() error { return m.resume(s, false) }, "")
	}
	if _, hosted := m.window(s); !hosted {
		m.setFlash(displayTitle(s)+" runs in another tab. Move it into deckk with m to write to it from here", nil)
		return nil
	}
	d := delivery{kind: typed, key: s.ID, target: s.TTY, text: text, send: send}
	// The page offers deckk's suggestion to edit there, so what the user
	// writes takes the place of the copy deckk typed into the prompt.
	if prev, ok := m.typedIn(s); ok {
		d.replace, d.at = prev.Text, prev.At
	}
	return m.deliver(d)
}

func (m *Model) chat(text string) tea.Cmd {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if !m.cfg.HQOn() {
		m.setFlash("The deckk session is off. Turn it on in settings", nil)
		return nil
	}
	if !m.deckk.open || !m.deckk.running {
		m.setFlash("Starting the deckk session. Send your message again once it's ready", nil)
		return m.do(func() error { return m.host.OpenHQ(*m.cfg, false) }, "")
	}
	d := delivery{kind: chat, target: m.deckk.pane.ID, text: text, send: true}
	if m.deckk.inflight[d.slot()] {
		m.setFlash("deckk is taking an update. Send your message again in a moment", nil)
		return nil
	}
	return m.deliver(d)
}

func (m Model) sessionByID(id string) (claude.Session, bool) {
	for _, s := range m.sessions {
		if s.ID == id {
			return s, true
		}
	}
	for _, s := range m.openProposals() {
		if s.ID == id {
			return s, true
		}
	}
	// Search reaches sessions older than the dashboard lists, so those
	// come from their transcript.
	home := m.home
	if home == "" {
		home = claude.Home()
	}
	if !claude.IsSessionID(id) {
		return claude.Session{}, false
	}
	if path := claude.TranscriptPath(home, id); path != "" {
		if t, err := claude.ReadTranscript(path); err == nil && !hq.Is(t.Session().Cwd) {
			s := []claude.Session{t.Session()}
			m.store.ApplyNames(s)
			return s[0], true
		}
	}
	return claude.Session{}, false
}

func (m Model) openWeb() tea.Cmd {
	if m.web == nil {
		return nil
	}
	url := m.web.URL()
	return m.do(func() error { return exec.Command("open", url).Run() }, "")
}

func (m Model) webState() web.State {
	now := time.Now()
	st := web.State{Clashes: []web.Clash{}, NextUp: []web.Next{}, Groups: []web.Group{}, Projects: []web.Group{}, Activity: []web.Event{}, Folders: abbreviated(m.folders, 300)}
	sessions := slices.DeleteFunc(slices.Clone(m.sessions), func(s claude.Session) bool { return s.Background })

	for _, s := range sessions {
		switch deck.StateOf(s) {
		case deck.YourTurn:
			st.Counts.Waiting++
		case deck.Working:
			st.Counts.Working++
		case deck.Stopped:
			if !m.store.Archived[s.ID] {
				st.Counts.Resumable++
			}
		}
	}

	for _, s := range m.nextUpSessions() {
		sg, _ := m.suggestion(s)
		kind := "suggestion"
		switch {
		case isProposal(s):
			kind = "proposal"
		case !s.Running():
			kind = "resume"
		}
		project := s.Project()
		if isProposal(s) {
			project = abbreviateHome(s.Dir)
		}
		st.NextUp = append(st.NextUp, web.Next{
			ID: s.ID, Kind: kind, Title: displayTitle(s), Project: project,
			Prompt: sg.Prompt, Why: sg.Why, Ready: m.prefilled(s, sg),
		})
	}

	for _, c := range m.collisions {
		if c.Clash() {
			st.Clashes = append(st.Clashes, web.Clash{A: displayTitle(c.A), B: displayTitle(c.B), Files: c.Files})
		}
	}

	for _, g := range deck.Arrange(sessions, m.marks(), "", false) {
		if limit, ok := m.collapseLimit(g); ok {
			g.Collapse(limit)
		}
		group := web.Group{Title: sentence(g.Title), Hidden: g.Hidden}
		for _, s := range g.Sessions {
			group.Sessions = append(group.Sessions, m.webSession(s, now))
		}
		st.Groups = append(st.Groups, group)
	}

	for _, g := range m.board() {
		g.Collapse(3)
		project := web.Group{Title: g.Title, Hidden: g.Hidden}
		if len(g.Sessions) > 0 {
			project.Folder = abbreviateHome(g.Sessions[0].Cwd)
		}
		for _, s := range g.Sessions {
			project.Sessions = append(project.Sessions, m.webSession(s, now))
		}
		st.Projects = append(st.Projects, project)
	}

	st.Goals = m.deckk.goals
	for _, e := range m.deckk.recent {
		st.Activity = append(st.Activity, web.Event{At: e.At, Text: e.Title + " " + e.Kind})
	}
	st.Today = today(sessions, now)
	st.DeckkState = m.deckkLabel()
	st.Deckk = web.Deckk{On: m.cfg.HQOn(), Running: m.deckk.running && m.deckk.open, Working: deck.StateOf(m.deckk.session) == deck.Working}
	if st.Deckk.Running {
		st.Deckk.ID = m.deckk.session.ID
	}
	st.Background = []web.Session{}
	for _, s := range m.sessions {
		if s.Background {
			st.Background = append(st.Background, m.webSession(s, now))
		}
	}
	slices.SortFunc(st.Background, func(a, b web.Session) int { return b.Since.Compare(a.Since) })
	st.Background = st.Background[:min(len(st.Background), 30)]
	if m.flash != "" {
		st.Notice = web.Notice{Text: m.flash, Error: m.flashErr, Seq: m.flashSeq}
	}
	return st
}

func (m Model) webSession(s claude.Session, now time.Time) web.Session {
	out := web.Session{
		ID: s.ID, Title: displayTitle(s), Project: s.Project(), Folder: abbreviateHome(s.Cwd),
		PR: s.PR, PRURL: s.PRURL, LastPrompt: clipText(s.LastPrompt, 400), LastReply: clipText(s.LastReply, 600),
		Context: s.Context, Changed: s.Git.Changed, Pinned: m.store.Pinned[s.ID], Since: s.UpdatedAt,
	}
	if s.Branch != "HEAD" {
		out.Branch = s.Branch
	}
	if s.Model != "" {
		out.Model = modelName(s.Model)
	}
	switch deck.StateOf(s) {
	case deck.Working:
		out.State, out.Since = "working", cmp.Or(s.StatusAt, s.UpdatedAt)
	case deck.YourTurn:
		out.State, out.Since = "waiting", cmp.Or(s.StatusAt, s.UpdatedAt)
	case deck.Stopped:
		out.State = "stopped"
		if at, ok := m.store.Closed[s.ID]; ok && now.Sub(at) < deck.ClosedFor {
			out.Status = "closed " + deck.For(at, now) + " ago"
		}
	default:
		out.State, out.Status = "other", s.Status
	}
	switch _, popped := m.poppedOut(s); {
	case !s.Running():
	case popped:
		out.Where = "own tab"
	case !m.inDeckk(s):
		out.Where = "another tab"
	}
	if sg, ok := m.suggestion(s); ok {
		out.Suggested, out.Suggestion, out.Why, out.Ready = true, sg.Prompt, sg.Why, m.prefilled(s, sg)
	}
	_, out.Hosted = m.window(s)
	out.Closable = m.closable(s)
	out.Running, out.Archived, out.Trusted = s.Running(), m.store.Archived[s.ID], m.store.Trusted[s.ID]
	if n, ok, stale := m.note(s); ok {
		out.Summary, out.Stale = clipText(n.Text, 240), stale
	}
	if c, ok := m.clash(s); ok {
		out.Clash = "also edited by " + displayTitle(c.Other(s.ID)) + ": " + fileList(c.Files, 3)
	}
	out.Reviewing = m.reviewing[s.ID]
	if r, ok := m.deckk.reviews[s.ID]; ok {
		out.Review = &web.Verdict{OK: r.OK, Concerns: concernLines(r.Text), At: r.At, Earlier: s.UpdatedAt.After(r.At)}
	}
	out.Changes = s.Git.Changed > 0 || s.Git.Ahead > 0 || s.PR != ""
	return out
}

func today(sessions []claude.Session, now time.Time) web.Today {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	byProject := map[string]int{}
	var t web.Today
	for _, s := range sessions {
		if !s.UpdatedAt.Before(midnight) {
			t.Touched++
			byProject[s.Project()]++
		}
		if s.PR != "" && now.Sub(s.UpdatedAt) < 7*24*time.Hour {
			t.PRs++
		}
	}
	t.Projects = len(byProject)
	if len(byProject) > 1 {
		t.Busiest = slices.MaxFunc(slices.Collect(maps.Keys(byProject)), func(a, b string) int {
			return cmp.Or(cmp.Compare(byProject[a], byProject[b]), cmp.Compare(b, a))
		})
	}
	return t
}

func (m Model) deckkLabel() string {
	switch {
	case !m.cfg.HQOn():
		return "off"
	case !m.deckk.open || !m.deckk.running:
		return "closed"
	case deck.StateOf(m.deckk.session) == deck.Working:
		return "working"
	}
	return "idle"
}

func sentence(title string) string {
	if title == "" {
		return title
	}
	lower := []rune(strings.ToLower(title))
	lower[0] = unicode.ToUpper(lower[0])
	return string(lower)
}

func abbreviated(dirs []string, limit int) []string {
	out := make([]string, 0, min(len(dirs), limit))
	for _, d := range dirs[:min(len(dirs), limit)] {
		out = append(out, abbreviateHome(d))
	}
	return out
}
