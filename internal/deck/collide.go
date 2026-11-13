package deck

import (
	"cmp"
	"slices"

	"github.com/theowecker/deckk/internal/claude"
)

// Sharing a checkout counts because a branch switch, a stash or a test run in
// one changes what the other sees.
type Collision struct {
	A, B     claude.Session
	Files    []string
	Checkout string
}

func (c Collision) Clash() bool { return len(c.Files) > 0 }

func (c Collision) Other(id string) claude.Session {
	if c.A.ID == id {
		return c.B
	}
	return c.A
}

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

func CollisionsOf(all []Collision, id string) []Collision {
	var mine []Collision
	for _, c := range all {
		if c.A.ID == id || c.B.ID == id {
			mine = append(mine, c)
		}
	}
	return mine
}
