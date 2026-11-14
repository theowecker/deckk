package ui

import (
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/deck"
	"github.com/theowecker/deckk/internal/hq"
)

type view int

const (
	homeView view = iota
	sessionsView
	boardView
	findView
	viewCount
)

const findLimit = 100

const backgroundShown = 5

func (v view) next() view { return (v + 1) % viewCount }
func (v view) prev() view { return (v + viewCount - 1) % viewCount }

func yours(sessions []claude.Session) []claude.Session {
	return slices.DeleteFunc(slices.Clone(sessions), func(s claude.Session) bool { return s.Background })
}

func (m Model) switchView(v view) (tea.Model, tea.Cmd) {
	if v == m.view {
		return m, nil
	}
	m.view, m.cursor, m.offset, m.selectedID = v, 0, 0, ""
	var cmd tea.Cmd
	if v == findView {
		cmd = tea.Batch(m.filter.Focus(), m.findEverything())
	} else {
		m.filter.Blur()
	}
	m.rearrange()
	return m, cmd
}

type foundMsg []claude.Session

// Find looks past the dashboard's time window, so it loads every transcript
// once on the way in; the transcript cache makes later visits cheap.
func (m Model) findEverything() tea.Cmd {
	return func() tea.Msg {
		sessions, err := claude.Load(m.home, time.Time{}, nil)
		if err != nil {
			return flashMsg{err: err}
		}
		m.store.Labels().ApplyNames(sessions)
		return foundMsg(slices.DeleteFunc(sessions, func(s claude.Session) bool { return hq.Is(s.Cwd) }))
	}
}

// Background runs go last and a few at a time, since a tool that calls claude
// -p can leave hundreds of them that would bury the session you came to resume.
func (m Model) findResults() []deck.Group {
	pool := m.everything
	if pool == nil {
		pool = m.sessions
	}
	query := m.filter.Value()
	mentioned := m.mentioned.ids
	if m.mentioned.query != query {
		mentioned = nil
	}
	var yours, said, background []claude.Session
	for _, s := range pool {
		matched := m.score(s, query) > 0
		switch {
		case s.Background && matched:
			background = append(background, s)
		case s.Background:
		case matched:
			yours = append(yours, s)
		case mentioned[s.ID]:
			said = append(said, s)
		}
	}
	deck.Rank(yours, func(s claude.Session) int { return m.score(s, query) })
	deck.Rank(said, func(claude.Session) int { return 0 })
	deck.Rank(background, func(s claude.Session) int { return m.score(s, query) })

	var groups []deck.Group
	if len(yours) > 0 {
		g := deck.Group{Section: deck.Recent, Title: "RESULTS", Sessions: yours}
		if query == "" {
			g.Title = "LATEST"
		}
		if len(yours) > findLimit {
			g.Sessions, g.Hidden = yours[:findLimit], len(yours)-findLimit
		}
		groups = append(groups, g)
	}
	if len(said) > 0 {
		g := deck.Group{Section: deck.Recent, Title: "MENTIONED IN THE CONVERSATION", Sessions: said}
		if len(said) > findLimit {
			g.Sessions, g.Hidden = said[:findLimit], len(said)-findLimit
		}
		groups = append(groups, g)
	}
	if len(background) > 0 {
		limit := backgroundShown
		if m.showAll || len(yours) == 0 {
			limit = findLimit
		}
		g := deck.Group{Section: deck.Recent, Title: "BACKGROUND", Sessions: background}
		if len(background) > limit {
			g.Sessions, g.Hidden = background[:limit], len(background)-limit
		}
		groups = append(groups, g)
	}
	return groups
}

func (m Model) updateFind(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.flash = ""
	switch msg.String() {
	case "esc":
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.rearrange()
			return m, nil
		}
		return m.switchView(homeView)
	case "tab":
		return m.switchView(m.view.next())
	case "shift+tab":
		return m.switchView(m.view.prev())
	case "up", "ctrl+p":
		m.move(-1)
		return m, nil
	case "down", "ctrl+n":
		m.move(1)
		return m, nil
	case "enter":
		if s, ok := m.selected(); ok {
			cmd := m.openWithSuggestion(s)
			return m, cmd
		}
		return m, nil
	case "ctrl+c":
		return m, m.detach()
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.cursor = 0
	m.rearrange()
	return m, tea.Batch(cmd, searchSoon(m.filter.Value()))
}

func (m Model) score(s claude.Session, query string) int {
	note, _, _ := m.note(s)
	return deck.Score(s, query, note.Text)
}

type mentionedMsg struct {
	query string
	ids   map[string]bool
}

type searchSoonMsg string

// Searching whole transcripts waits for a pause in typing.
func searchSoon(query string) tea.Cmd {
	if len(strings.TrimSpace(query)) < 3 {
		return nil
	}
	return tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg { return searchSoonMsg(query) })
}

func (m Model) searchTranscripts(query string) tea.Cmd {
	pool := m.everything
	if pool == nil {
		pool = m.sessions
	}
	byPath := map[string]string{}
	var paths []string
	for _, s := range pool {
		if s.Path != "" && !s.Background {
			byPath[s.Path] = s.ID
			paths = append(paths, s.Path)
		}
	}
	return func() tea.Msg {
		found, err := claude.Mentioning(paths, query)
		if err != nil {
			return nil
		}
		ids := map[string]bool{}
		for path := range found {
			ids[byPath[path]] = true
		}
		return mentionedMsg{query, ids}
	}
}

type viewTab struct {
	view  view
	label string
}

func (m Model) viewTabs() []viewTab {
	return []viewTab{{homeView, "Home"}, {sessionsView, "Sessions"}, {boardView, "Board"}, {findView, "Find"}}
}

const viewTabGap = "    "

func (m Model) viewTabsLine() string {
	var parts []string
	for _, t := range m.viewTabs() {
		if t.view == m.view {
			parts = append(parts, accent.Bold(true).Underline(true).Render(t.label))
		} else {
			parts = append(parts, dim.Render(t.label))
		}
	}
	line := pad() + strings.Join(parts, viewTabGap)
	if m.web != nil {
		line += viewTabGap + viewTabGap + working.Render(webLink)
	}
	return line
}

const webLink = "Web ↗"

func (m Model) webLinkAt(x, y int) bool {
	if y != 0 || m.web == nil {
		return false
	}
	start := lipgloss.Width(pad() + strings.Join(m.viewTabLabels(), viewTabGap) + viewTabGap + viewTabGap)
	return x >= start && x < start+lipgloss.Width(webLink)
}

func (m Model) viewTabLabels() []string {
	var labels []string
	for _, t := range m.viewTabs() {
		labels = append(labels, t.label)
	}
	return labels
}

// Must mirror viewTabsLine.
func (m Model) viewTabAt(x, y int) (view, bool) {
	if y != 0 {
		return 0, false
	}
	start := gutter
	for _, t := range m.viewTabs() {
		end := start + lipgloss.Width(t.label)
		if x >= start && x < end {
			return t.view, true
		}
		start = end + len(viewTabGap)
	}
	return 0, false
}
