package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/deck"
)

// The band under the tab strip, in tmux style markup. Colors match the strip
// in tmux/config.go, and band must match its status-format.
const band = "#24243A"

// tabInfo describes a hosted session: what it's doing and where on the left,
// how Claude is set up on the right.
func tabInfo(s claude.Session, now time.Time) string {
	return " " + chip(s, now) + "  " + strings.Join(where(s), "   ") +
		"#[align=right]" + strings.Join(setup(s, now), "#[fg=#4A4A5E] · ") + "  "
}

func chip(s claude.Session, now time.Time) string {
	since := ""
	if !s.StatusAt.IsZero() && now.Sub(s.StatusAt) >= time.Minute {
		since = " " + deck.For(s.StatusAt, now)
	}
	bg, label := "#4A4A5E", s.Status
	switch deck.StateOf(s) {
	case deck.Working:
		bg, label = "#7AA2F7", "◐ working"
	case deck.YourTurn:
		bg, label = "#E8875B", "● waiting"
	}
	return "#[bg=" + bg + ",fg=#1E1E2E,bold] " + tmuxText(label+since) + " #[bg=" + band + ",nobold]"
}

func where(s claude.Session) []string {
	g := s.Git
	dir := s.Cwd
	if g.Worktree != "" {
		dir = g.Repo
	}
	parts := []string{styled("#B4B4C0", tilde(dir))}
	if s.Branch != "" && s.Branch != "HEAD" {
		branch := styled("#ECECEC", "⎇ "+s.Branch)
		if g.Changed > 0 {
			branch += styled("#E0AF68", " ±"+strconv.Itoa(g.Changed))
		}
		if g.Ahead > 0 {
			branch += styled("#9ECE6A", " ↑"+strconv.Itoa(g.Ahead))
		}
		if g.Behind > 0 {
			branch += styled("#F97066", " ↓"+strconv.Itoa(g.Behind))
		}
		parts = append(parts, branch)
	}
	if g.Worktree != "" {
		parts = append(parts, styled("#8A8A9A", "worktree ")+styled("#B4B4C0", g.Worktree))
	}
	if s.PR != "" {
		parts = append(parts, styled("#E8875B", "PR "+s.PR))
	}
	return parts
}

func setup(s claude.Session, now time.Time) []string {
	var parts []string
	if s.Model != "" {
		model := styled("#B4B4C0", modelName(s.Model))
		if s.Effort != "" {
			model += styled("#8A8A9A", " "+s.Effort)
		}
		parts = append(parts, model)
	}
	switch s.Mode {
	case "", "default":
	case "bypassPermissions":
		parts = append(parts, styled("#F97066", "bypassing permissions"))
	case "plan":
		parts = append(parts, styled("#7AA2F7", "plan mode"))
	case "acceptEdits":
		parts = append(parts, styled("#8A8A9A", "accepting edits"))
	default:
		parts = append(parts, styled("#8A8A9A", s.Mode+" mode"))
	}
	if s.Context > 0 {
		parts = append(parts, styled("#8A8A9A", tokens(s.Context)+" context"))
	}
	if !s.StartedAt.IsZero() {
		parts = append(parts, styled("#8A8A9A", "up "+deck.For(s.StartedAt, now)))
	}
	return parts
}

func styled(color, text string) string { return "#[fg=" + color + "]" + tmuxText(text) }

// claude-opus-5-5 reads as opus 5.5. Dated ids drop the date.
func modelName(id string) string {
	var name, version []string
	for _, part := range strings.Split(strings.TrimPrefix(id, "claude-"), "-") {
		switch _, err := strconv.Atoi(part); {
		case err != nil:
			name = append(name, part)
		case len(part) < 8:
			version = append(version, part)
		}
	}
	return strings.TrimSpace(strings.Join(name, " ") + " " + strings.Join(version, "."))
}

func tokens(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	return fmt.Sprintf("%dk", (n+500)/1000)
}

func tilde(dir string) string {
	home, _ := os.UserHomeDir()
	if rel, err := filepath.Rel(home, dir); home != "" && err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.Join("~", rel)
	}
	return dir
}

// tmux reads # as a format or style.
func tmuxText(s string) string { return strings.ReplaceAll(s, "#", "##") }
