package ui

import (
	"cmp"
	"maps"
	"os/exec"
	"slices"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/deck"
	"github.com/theowecker/deckk/internal/web"
)

// A Publisher shows the dashboard's state somewhere else, such as the page
// deckk serves on localhost.
type Publisher interface {
	Publish(state any)
	URL() string
}

// WithWeb has the dashboard publish to the page and take its actions.
func (m Model) WithWeb(p Publisher) Model {
	m.web = p
	return m
}

type webActionMsg web.Action

// WebAction carries an action from the page into the dashboard.
func WebAction(a web.Action) tea.Msg { return webActionMsg(a) }

func (m Model) publish() {
	if m.web != nil {
		m.web.Publish(m.webState())
	}
}

// webAction does what the page asked, the same way the keys would.
func (m *Model) webAction(a webActionMsg) tea.Cmd {
	if a.Kind == web.Talk {
		return m.openDeckk()
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
	}
	return nil
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
	st := web.State{Clashes: []web.Clash{}, NextUp: []web.Next{}, Groups: []web.Group{}, Activity: []web.Event{}}
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

	st.Goals = m.deckk.goals
	for _, e := range m.deckk.recent {
		st.Activity = append(st.Activity, web.Event{At: e.At, Text: e.Title + " " + e.Kind})
	}
	st.Today = today(sessions, now)
	st.DeckkState = m.deckkLabel()
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
	_, out.Suggested = m.suggestion(s)
	if n, ok, stale := m.note(s); ok {
		out.Summary, out.Stale = n.Text, stale
	}
	if c, ok := m.clash(s); ok {
		out.Clash = "also edited by " + displayTitle(c.Other(s.ID)) + ": " + fileList(c.Files, 3)
	}
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

// sentence turns the terminal's section titles into the page's sentence case.
func sentence(title string) string {
	if title == "" {
		return title
	}
	lower := []rune(strings.ToLower(title))
	lower[0] = unicode.ToUpper(lower[0])
	return string(lower)
}
