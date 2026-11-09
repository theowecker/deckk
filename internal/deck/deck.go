package deck

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/theowecker/deckk/internal/claude"
)

type State int

const (
	Stopped State = iota
	Working
	YourTurn
	Other
)

// "shell" means Claude is running a long command, which is still working.
func StateOf(s claude.Session) State {
	switch {
	case !s.Running():
		return Stopped
	case s.Status == "busy" || s.Status == "shell":
		return Working
	case s.Status == "idle":
		return YourTurn
	default:
		return Other
	}
}

type Section int

const (
	Running Section = iota
	Elsewhere
	Closed
	Pinned
	Recent
	Archived
)

func (s Section) String() string {
	return [...]string{"RUNNING", "IN OTHER TABS", "RECENTLY CLOSED", "PINNED", "RECENT", "ARCHIVED"}[s]
}

const ClosedFor = 24 * time.Hour

type Group struct {
	Section  Section
	Title    string
	Sessions []claude.Session
}

type Marks struct {
	Pinned   map[string]bool
	Archived map[string]bool
	Open     map[string]bool
	// A nil Hosted counts every running session as hosted, for callers that
	// can't see tmux.
	Hosted map[string]bool
	Order  map[string]int
	Closed map[string]time.Time
	Now    time.Time
}

func (m Marks) recentlyClosed(s claude.Session) bool {
	at, ok := m.Closed[s.ID]
	return ok && !s.Running() && m.Now.Sub(at) < ClosedFor
}

func (m Marks) section(s claude.Session) Section {
	switch {
	case s.Running() && m.Hosted != nil && !m.Hosted[s.ID]:
		return Elsewhere
	case s.Running():
		return Running
	case m.Archived[s.ID]:
		return Archived
	case m.recentlyClosed(s):
		return Closed
	case m.Pinned[s.ID]:
		return Pinned
	default:
		return Recent
	}
}

func Arrange(sessions []claude.Session, marks Marks, query string, showArchived bool) []Group {
	buckets := make([][]claude.Session, Archived+1)
	for _, s := range sessions {
		sec := marks.section(s)
		if sec == Archived && !showArchived || !Matches(s, query) {
			continue
		}
		buckets[sec] = append(buckets[sec], s)
	}

	var groups []Group
	for sec, list := range buckets {
		if len(list) == 0 {
			continue
		}
		slices.SortFunc(list, marks.order(Section(sec)))
		groups = append(groups, Group{Section: Section(sec), Title: Section(sec).String(), Sessions: list})
	}
	return groups
}

func ArrangeByProject(sessions []claude.Session, marks Marks, query string, showArchived bool) []Group {
	byProject := map[string][]claude.Session{}
	for _, s := range sessions {
		if marks.section(s) == Archived && !showArchived || !Matches(s, query) {
			continue
		}
		byProject[s.Project()] = append(byProject[s.Project()], s)
	}

	var groups []Group
	for name, list := range byProject {
		slices.SortFunc(list, func(a, b claude.Session) int {
			return cmp.Or(
				compareBool(b.Running(), a.Running()),
				b.UpdatedAt.Compare(a.UpdatedAt),
				strings.Compare(a.ID, b.ID),
			)
		})
		groups = append(groups, Group{Section: Recent, Title: strings.ToUpper(name), Sessions: list})
	}
	slices.SortFunc(groups, func(a, b Group) int {
		return cmp.Or(
			cmp.Compare(running(b.Sessions), running(a.Sessions)),
			b.Sessions[0].UpdatedAt.Compare(a.Sessions[0].UpdatedAt),
			strings.Compare(a.Title, b.Title),
		)
	})
	return groups
}

func running(sessions []claude.Session) int {
	n := 0
	for _, s := range sessions {
		if s.Running() {
			n++
		}
	}
	return n
}

// Running sessions follow the tab strip so rows don't jump as work happens.
func (m Marks) order(sec Section) func(a, b claude.Session) int {
	return func(a, b claude.Session) int {
		switch sec {
		case Running:
			return cmp.Or(cmp.Compare(m.Order[a.ID], m.Order[b.ID]), strings.Compare(a.ID, b.ID))
		case Elsewhere:
			return cmp.Or(a.StartedAt.Compare(b.StartedAt), strings.Compare(a.ID, b.ID))
		case Closed:
			return cmp.Or(m.Closed[b.ID].Compare(m.Closed[a.ID]), strings.Compare(a.ID, b.ID))
		}
		return cmp.Or(
			compareBool(m.Pinned[b.ID], m.Pinned[a.ID]),
			b.UpdatedAt.Compare(a.UpdatedAt),
			strings.Compare(a.ID, b.ID),
		)
	}
}

func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	default:
		return -1
	}
}

func Matches(s claude.Session, query string) bool {
	haystack := strings.ToLower(strings.Join([]string{s.Title, s.Project(), s.Branch, s.LastPrompt}, " "))
	for word := range strings.FieldsSeq(strings.ToLower(query)) {
		if !strings.Contains(haystack, word) {
			return false
		}
	}
	return true
}

func ToRestore(sessions []claude.Session, marks Marks) []claude.Session {
	var out []claude.Session
	for _, s := range sessions {
		wanted := marks.Pinned[s.ID] || marks.Open[s.ID] || marks.recentlyClosed(s)
		if wanted && !s.Running() && !marks.Archived[s.ID] {
			out = append(out, s)
		}
	}
	return out
}

func For(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw", int(d.Hours()/24/7))
	}
}

func Ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return t.Format("Jan 2")
	}
}
