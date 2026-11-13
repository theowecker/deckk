package ui

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/deck"
)

// The board leaves out projects nobody touched in this long, unless one of
// their sessions is running or pinned.
const boardFor = 7 * 24 * time.Hour

func (m Model) board() []deck.Group {
	now := time.Now()
	recent := slices.DeleteFunc(slices.Clone(m.sessions), func(s claude.Session) bool {
		return s.Background || !s.Running() && !m.store.Pinned[s.ID] && now.Sub(s.UpdatedAt) > boardFor
	})
	groups := deck.ArrangeByProject(recent, m.marks(), "", false)
	running := func(g deck.Group) int {
		n := 0
		for _, s := range g.Sessions {
			if s.Running() {
				n++
			}
		}
		return n
	}
	slices.SortStableFunc(groups, func(a, b deck.Group) int { return running(b) - running(a) })
	return groups
}

func (m Model) projectTitle(g deck.Group) string {
	var waiting, busy int
	for _, s := range g.Sessions {
		switch deck.StateOf(s) {
		case deck.YourTurn:
			waiting++
		case deck.Working:
			busy++
		}
	}
	var counts []string
	if waiting > 0 {
		counts = append(counts, accent.Render(fmt.Sprintf("%d waiting", waiting)))
	}
	if busy > 0 {
		counts = append(counts, working.Render(fmt.Sprintf("%d working", busy)))
	}
	if len(counts) == 0 {
		counts = append(counts, ghost.Render(sessions(len(g.Sessions)+g.Hidden)))
	}
	head := heading.Render(g.Title) + "  " + strings.Join(counts, ghost.Render(" · "))
	line := rule.Render(strings.Repeat("─", max(m.width-gutter*2-lipgloss.Width(head)-1, 2)))
	return pad() + head + " " + line
}

// Shows the session's last prompt until the deckk session has written a note on
// it.
func (m Model) boardRow(s claude.Session, selected bool, now time.Time, titles int) string {
	bar, titleStyle := "  ", soft
	if s.Running() {
		titleStyle = text
	}
	if selected {
		bar, titleStyle = accent.Render("▌ "), bold
	}
	glyph, status := m.status(s, now)
	room := m.rowRoom()
	titles = min(titles, room*2/5)
	title := ansi.Truncate(rowTitle(s), titles, "…")
	line := titleStyle.Render(title) + strings.Repeat(" ", titles-lipgloss.Width(title))
	about := ""
	switch n, ok, stale := m.note(s); {
	case ok && !stale:
		about = soft.Render(n.Text)
	case ok:
		about = dim.Render(n.Text)
	case s.LastPrompt != "":
		about = ghost.Render("› " + s.LastPrompt)
	}
	if aboutRoom := room - titles - 3; aboutRoom > 3 && about != "" {
		about = ansi.Truncate(about, aboutRoom, "…")
		line += "   " + about + strings.Repeat(" ", aboutRoom-lipgloss.Width(about))
	} else {
		line += strings.Repeat(" ", max(room-titles, 0))
	}
	return bar + glyph + " " + line + "  " + lipgloss.PlaceHorizontal(statusWidth, lipgloss.Right, status)
}

var markdownMarks = regexp.MustCompile(`^\s*(#+\s*|[-*]\s+(\[[ x]\]\s+)?)|\*\*|` + "`")

func (m Model) goalLines() []string {
	const shown = 6
	head := pad() + heading.Render("GOALS") + " " + rule.Render(strings.Repeat("─", max(m.width-gutter*2-7, 2)))
	if !m.cfg.HQOn() {
		return nil
	}
	var goals []string
	for raw := range strings.Lines(m.deckk.goals) {
		if line := strings.TrimSpace(markdownMarks.ReplaceAllString(raw, "")); line != "" {
			goals = append(goals, line)
		}
	}
	lines := []string{head}
	if len(goals) == 0 {
		lines = append(lines, pad()+dim.Render("No goals yet. Tell the deckk session what you're working toward; press c."))
	}
	for i, g := range goals {
		if i == shown {
			lines = append(lines, pad()+ghost.Render(fmt.Sprintf("+ %d more in goals.md", len(goals)-shown)))
			break
		}
		lines = append(lines, pad()+"  "+text.Render(ansi.Truncate(g, m.width-gutter*2-2, "…")))
	}
	return append(lines, "")
}
