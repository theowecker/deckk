package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/git"
)

// Work counts as left behind once its last session has been quiet this long,
// and stops counting once it's old enough to be deliberate.
const (
	leftAfter   = 3 * 24 * time.Hour
	leftUntil   = 30 * 24 * time.Hour
	leftCheckIn = 10 * time.Minute
)

type leftBehind struct {
	session claude.Session
	git     git.Status
	why     string
}

type leftBehindMsg []leftBehind

// findLeftBehind looks for checkouts nobody is working in that still hold
// uncommitted or unpushed work, and PRs left open. Reading every checkout's
// git status is slow, so it runs off the dashboard's goroutine, rarely.
func (m *Model) findLeftBehind(now time.Time) tea.Cmd {
	if now.Sub(m.leftAt) < leftCheckIn {
		return nil
	}
	m.leftAt = now
	pool := slices.Clone(m.sessions)
	return func() tea.Msg { return leftBehindMsg(leftBehindIn(pool, now)) }
}

func leftBehindIn(sessions []claude.Session, now time.Time) []leftBehind {
	active := map[string]bool{}
	latest := map[string]claude.Session{}
	for _, s := range sessions {
		if s.Background || s.Cwd == "" {
			continue
		}
		dir := filepath.Clean(s.Cwd)
		if s.Running() {
			active[dir] = true
		}
		if s.UpdatedAt.After(latest[dir].UpdatedAt) {
			latest[dir] = s
		}
	}
	var out []leftBehind
	for dir, s := range latest {
		quiet := now.Sub(s.UpdatedAt)
		if active[dir] || quiet < leftAfter || quiet > leftUntil || !isDir(dir) {
			continue
		}
		g, ok := git.Read(dir)
		var why []string
		if ok && g.Changed > 0 {
			why = append(why, plural(g.Changed, "uncommitted change"))
		}
		if ok && g.Ahead > 0 {
			why = append(why, plural(g.Ahead, "unpushed commit"))
		}
		if s.PR != "" && !s.PRStatus.Done() && s.PRStatus.State != "" {
			why = append(why, "PR "+s.PR+" still open")
		}
		if len(why) > 0 {
			out = append(out, leftBehind{session: s, git: g, why: strings.Join(why, ", ")})
		}
	}
	slices.SortFunc(out, func(a, b leftBehind) int { return b.session.UpdatedAt.Compare(a.session.UpdatedAt) })
	return out
}

func (l leftBehind) line(now time.Time) string {
	where := abbreviateHome(l.session.Cwd)
	if l.git.Branch != "" && l.git.Branch != "HEAD" {
		where += " ⎇ " + l.git.Branch
	}
	return fmt.Sprintf("%s: %s, untouched %s", where, l.why, quietFor(now.Sub(l.session.UpdatedAt)))
}

func quietFor(d time.Duration) string {
	days := int(d.Hours() / 24)
	if days < 14 {
		return plural(days, "day")
	}
	return plural(days/7, "week")
}
