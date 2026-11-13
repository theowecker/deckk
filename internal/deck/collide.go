package deck

import (
	"cmp"
	"slices"

	"github.com/theowecker/deckk/internal/claude"
)

// A Collision is two running sessions that could undo each other's work:
// both edited the same files, or both work in one checkout, where a branch
// switch, a stash or a test run in one changes what the other sees.
type Collision struct {
	A, B claude.Session
	// Files both have edited since they started.
	Files []string
	// Checkout is the working tree they share, if they share one.
	Checkout string
}

// Clash is the serious kind: the same files.
func (c Collision) Clash() bool { return len(c.Files) > 0 }

// Other is the session on the far side of a collision from id.
func (c Collision) Other(id string) claude.Session {
	if c.A.ID == id {
		return c.B
	}
	return c.A
}

// Collisions pairs up running sessions that edit the same files or share a
// checkout, clashes first. edits are each session's edited files by ID, and
// root finds the checkout a folder belongs to.
func Collisions(sessions []claude.Session, edits map[string][]string, root func(string) string) []Collision {
	var live []claude.Session
	roots := map[string]string{}
	for _, s := range sessions {
		if s.Running() && !s.Background {
			live = append(live, s)
			roots[s.ID] = root(s.Cwd)
		}
	}
	slices.SortFunc(live, func(a, b claude.Session) int { return cmp.Compare(a.ID, b.ID) })
	var found []Collision
	for i, a := range live {
		for _, b := range live[i+1:] {
			c := Collision{A: a, B: b, Files: shared(edits[a.ID], edits[b.ID])}
			if roots[a.ID] != "" && roots[a.ID] == roots[b.ID] {
				c.Checkout = roots[a.ID]
			}
			if c.Clash() || c.Checkout != "" {
				found = append(found, c)
			}
		}
	}
	slices.SortStableFunc(found, func(x, y Collision) int { return cmp.Compare(len(y.Files), len(x.Files)) })
	return found
}

func shared(a, b []string) []string {
	var both []string
	for _, f := range a {
		if slices.Contains(b, f) {
			both = append(both, f)
		}
	}
	return both
}

// CollisionsOf lists the collisions one session is part of.
func CollisionsOf(all []Collision, id string) []Collision {
	var mine []Collision
	for _, c := range all {
		if c.A.ID == id || c.B.ID == id {
			mine = append(mine, c)
		}
	}
	return mine
}
