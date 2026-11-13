package ui

import (
	"fmt"
	"strings"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/deck"
	"github.com/theowecker/deckk/internal/hq"
)

type goalProgress struct {
	title    string
	sessions []claude.Session
}

// goals are the goals in goals.md, each with the sessions linked to it.
func (m Model) goals() []goalProgress {
	var out []goalProgress
	for _, title := range hq.GoalsIn(m.deckk.goals) {
		g := goalProgress{title: title}
		for _, s := range m.sessions {
			if m.deckk.links[s.ID] == title {
				g.sessions = append(g.sessions, s)
			}
		}
		out = append(out, g)
	}
	return out
}

// summary says how a goal's sessions stand: how many are waiting or working,
// how their PRs are doing and what reviews flagged.
func (m Model) summary(g goalProgress) string {
	if len(g.sessions) == 0 {
		return "no sessions linked yet"
	}
	var waiting, working, merged, open, needsYou, flagged int
	for _, s := range g.sessions {
		switch deck.StateOf(s) {
		case deck.YourTurn:
			waiting++
		case deck.Working:
			working++
		}
		switch label, weight := prLabel(s.PRStatus); {
		case label == "merged":
			merged++
		case weight == 2:
			needsYou++
		case s.PR != "" && !s.PRStatus.Done():
			open++
		}
		if r, ok := m.deckk.reviews[s.ID]; ok && !r.OK && !s.UpdatedAt.After(r.At) {
			flagged++
		}
	}
	parts := []string{plural(len(g.sessions), "session")}
	add := func(n int, what string) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, what))
		}
	}
	add(waiting, "waiting")
	add(working, "working")
	add(needsYou, pick(needsYou, "PR needs you", "PRs need you"))
	add(open, pick(open, "PR open", "PRs open"))
	add(merged, pick(merged, "PR merged", "PRs merged"))
	add(flagged, "flagged by review")
	return strings.Join(parts, " · ")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// goalNamed is the goal a session works toward, if it's still in goals.md.
func (m Model) goalOf(s claude.Session) string {
	goal := m.deckk.links[s.ID]
	for _, g := range hq.GoalsIn(m.deckk.goals) {
		if g == goal {
			return goal
		}
	}
	return ""
}

func pick(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
