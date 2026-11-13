package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/deck"
)

// Colours match the strip in tmux/config.go, and band must match its
// status-format.
const band = "#24243A"

type bandInfo struct {
	width      int
	pinned     bool
	suggestion string
	// warning is a collision with another session, and clash says it is
	// over the same files rather than a shared checkout.
	warning string
	clash   bool
}

// The setup on the right drops its last details until the band fits.
func tabInfo(s claude.Session, now time.Time, b bandInfo) string {
	left := " " + chip(s, now) + "  "
	switch {
	case b.warning != "" && b.clash:
		left += styled("#E0AF68", "⚠ "+clipText(b.warning, 70)) + "   "
	case b.warning != "":
		left += styled("#8A8A9A", "⇄ "+clipText(b.warning, 50)) + "   "
	}
	if b.suggestion != "" {
		left += styled("#E8875B", "✦ deckk suggests: "+clipText(b.suggestion, 70)) + "   "
	}
	left += strings.Join(where(s), "   ")
	button := pinButton(b.pinned)
	width := b.width
	details := setup(s, now)
	for {
		right := strings.Join(details, "#[fg=#4A4A5E] · ")
		if right != "" {
			right += "  "
		}
		right += button
		if len(details) == 0 || bandWidth(left)+bandWidth(right)+4 <= width {
			return left + "#[align=right]" + right
		}
		details = details[:len(details)-1]
	}
}

func pinButton(pinned bool) string {
	label := " pin "
	if pinned {
		label = " unpin "
	}
	return "#[range=user|pin]#[bg=#30304A,fg=#B4B4C0]" + label + "#[norange bg=" + band + "] "
}

var markup = regexp.MustCompile(`#\[[^\]]*\]`)

func bandWidth(s string) int {
	return ansi.StringWidth(strings.ReplaceAll(markup.ReplaceAllString(s, ""), "##", "#"))
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
