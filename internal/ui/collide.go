package ui

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/deck"
	"github.com/theowecker/deckk/internal/hq"
)

// trackCollisions turns clashes the deckk session hasn't heard of into
// events. A pair is news when it first clashes and again only when it shares
// a file it didn't before, so two sessions working on through a clash don't
// cost a turn per refresh. Sharing a checkout is often on purpose, so only
// clashes count, and the ones already there when deckk opens are known.
func (m *Model) trackCollisions() []hq.Event {
	now := time.Now()
	current := map[string]map[string]bool{}
	var events []hq.Event
	for _, c := range m.collisions {
		if !c.Clash() {
			continue
		}
		pair := c.A.ID + "+" + c.B.ID
		known := m.clashes[pair]
		files := map[string]bool{}
		fresh := false
		for _, f := range c.Files {
			files[f] = true
			fresh = fresh || !known[f]
		}
		current[pair] = files
		if fresh && m.clashesSeen {
			events = append(events, hq.Event{
				At: now, Session: c.A.ID, Title: displayTitle(c.A),
				Kind:   "edited the same files as " + displayTitle(c.B) + " [" + claude.ShortID(c.B.ID) + "]",
				Detail: fileList(c.Files, 5),
			})
		}
	}
	m.clashes, m.clashesSeen = current, true
	return events
}

// clash is the first clash a session is in, if any.
func (m Model) clash(s claude.Session) (deck.Collision, bool) {
	for _, c := range deck.CollisionsOf(m.collisions, s.ID) {
		if c.Clash() {
			return c, true
		}
	}
	return deck.Collision{}, false
}

// collisionLines describe a session's collisions for its preview.
func (m Model) collisionLines(s claude.Session) []string {
	var lines []string
	for _, c := range deck.CollisionsOf(m.collisions, s.ID) {
		other := displayTitle(c.Other(s.ID))
		if c.Clash() {
			lines = append(lines, "⚠ "+other+" also edited "+fileList(c.Files, 3))
		} else {
			lines = append(lines, "⇄ shares "+abbreviateHome(c.Checkout)+" with "+other)
		}
	}
	return lines
}

// clashBanner is the warning above the list, one line per clash.
func (m Model) clashBanner() []string {
	var lines []string
	for _, c := range m.collisions {
		if c.Clash() {
			lines = append(lines, displayTitle(c.A)+" and "+displayTitle(c.B)+" both edited "+fileList(c.Files, 3))
		}
	}
	return lines
}

// fileList names files by their base names, so a line stays readable.
func fileList(files []string, limit int) string {
	names := make([]string, 0, len(files))
	for _, f := range files {
		if name := filepath.Base(f); !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	if len(names) > limit {
		return strings.Join(names[:limit], ", ") + " and " + strconv.Itoa(len(names)-limit) + " more"
	}
	return strings.Join(names, ", ")
}
