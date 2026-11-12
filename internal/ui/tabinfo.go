package ui

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/deck"
)

// tabInfo is the line under the tab strip, in tmux style markup. Colors match
// the strip in tmux/config.go.
func tabInfo(s claude.Session, now time.Time) string {
	var parts []string
	add := func(color, text string) {
		parts = append(parts, "#[fg="+color+"]"+tmuxText(text))
	}

	since := ""
	if !s.StatusAt.IsZero() && now.Sub(s.StatusAt) >= time.Minute {
		since = " " + deck.For(s.StatusAt, now)
	}
	switch deck.StateOf(s) {
	case deck.Working:
		add("#7AA2F7", "working"+since)
	case deck.YourTurn:
		add("#E8875B", "your turn"+since)
	default:
		add("#8A8A8A", s.Status)
	}

	dir := s.Cwd
	if s.Worktree != "" {
		dir = s.Repo
	}
	add("#8A8A8A", tilde(dir))
	if s.Branch != "" && s.Branch != "HEAD" {
		add("#B4B4C0", "⎇ "+s.Branch)
	}
	if s.Worktree != "" && s.Worktree != s.Branch {
		add("#B4B4C0", "worktree "+s.Worktree)
	} else if s.Worktree != "" {
		add("#6B6B6B", "worktree")
	}
	if s.PR != "" {
		add("#E8875B", "PR "+s.PR)
	}
	return "  " + strings.Join(parts, "#[fg=#4A4A4A]  ·  ")
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
