package ui

import (
	"cmp"
	"fmt"
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
	sessionsView view = iota
	// boardView is the overview: goals, then each project's sessions with
	// what the deckk session says about them.
	boardView
	backgroundView
	findView
	viewCount
)

const findLimit = 100

// Background runs Find shows before + shows them all.
const backgroundShown = 5

func (v view) next() view { return (v + 1) % viewCount }
func (v view) prev() view { return (v + viewCount - 1) % viewCount }

func (m Model) inView(sessions []claude.Session) []claude.Session {
	background := m.view == backgroundView
	return slices.DeleteFunc(slices.Clone(sessions), func(s claude.Session) bool { return s.Background != background })
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
		m.store.ApplyNames(sessions)
		return foundMsg(slices.DeleteFunc(sessions, func(s claude.Session) bool { return hq.Is(s.Cwd) }))
	}
}

// findResults lists your own sessions first and background runs after,
// a few at a time, since a tool that calls claude -p can leave hundreds of
// them that would bury the session you came to resume.
func (m Model) findResults() []deck.Group {
	pool := m.everything
	if pool == nil {
		pool = m.sessions
	}
	query := m.filter.Value()
	var yours, background []claude.Session
	for _, s := range pool {
		switch {
		case !deck.Matches(s, query):
		case s.Background:
			background = append(background, s)
		default:
			yours = append(yours, s)
		}
	}
	newest := func(a, b claude.Session) int {
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), strings.Compare(a.ID, b.ID))
	}
	slices.SortFunc(yours, newest)
	slices.SortFunc(background, newest)

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
		return m.switchView(sessionsView)
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
	return m, cmd
}

type viewTab struct {
	view  view
	label string
}

func (m Model) viewTabs() []viewTab {
	background := 0
	for _, s := range m.sessions {
		if s.Background {
			background++
		}
	}
	label := "Background"
	if background > 0 {
		label = fmt.Sprintf("Background %d", background)
	}
	return []viewTab{{sessionsView, "Sessions"}, {boardView, "Board"}, {backgroundView, label}, {findView, "Find"}}
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

// webLink opens the page deckk serves, from the end of the view tabs.
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
