package ui

import (
	"slices"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/deck"
)

// homeRecent is how many stopped sessions Home keeps in view; Sessions has
// the rest.
const homeRecent = 3

// homeGroups is what to glance at: suggestions, what needs the user, what's
// working, and the last few sessions touched.
func (m Model) homeGroups() []deck.Group {
	var groups []deck.Group
	suggested := map[string]bool{}
	if next, ok := m.nextUp(); ok {
		groups = append(groups, next)
		for _, s := range next.Sessions {
			suggested[s.ID] = true
		}
	}
	var needsYou, busy, recent []claude.Session
	for _, s := range yours(m.sessions) {
		switch {
		case suggested[s.ID]:
		case s.Running() && deck.StateOf(s) == deck.YourTurn, m.flagged(s):
			needsYou = append(needsYou, s)
		case s.Running():
			busy = append(busy, s)
		case !m.store.Archived[s.ID]:
			recent = append(recent, s)
		}
	}
	newest := func(a, b claude.Session) int { return b.UpdatedAt.Compare(a.UpdatedAt) }
	slices.SortFunc(needsYou, newest)
	slices.SortFunc(busy, newest)
	slices.SortFunc(recent, newest)
	add := func(section deck.Section, list []claude.Session) {
		if len(list) > 0 {
			groups = append(groups, deck.Group{Section: section, Title: section.String(), Sessions: list})
		}
	}
	add(deck.NeedsYou, needsYou)
	add(deck.Busy, busy)
	if len(recent) > homeRecent {
		groups = append(groups, deck.Group{Section: deck.Recent, Title: deck.Recent.String(), Sessions: recent[:homeRecent], Hidden: len(recent) - homeRecent})
	} else {
		add(deck.Recent, recent)
	}
	return groups
}

// flagged is a session whose PR or latest review wants the user's attention.
func (m Model) flagged(s claude.Session) bool {
	if _, weight := prLabel(s.PRStatus); weight == 2 {
		return true
	}
	r, ok := m.deckk.reviews[s.ID]
	return ok && !r.OK && !s.UpdatedAt.After(r.At)
}

// background is the runs nobody typed into, such as claude -p, kept below
// everything else in Sessions.
func (m Model) background() deck.Group {
	var runs []claude.Session
	for _, s := range m.sessions {
		if s.Background {
			runs = append(runs, s)
		}
	}
	slices.SortFunc(runs, func(a, b claude.Session) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	return deck.Group{Section: deck.Background, Title: deck.Background.String(), Sessions: runs}
}
