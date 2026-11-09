package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/config"
	"github.com/theowecker/deckk/internal/store"
	"github.com/theowecker/deckk/internal/tmux"
)

// Renders the README screenshot from demo data. Run script/screenshot.
func TestScreenshot(t *testing.T) {
	out := os.Getenv("DECKK_SCREENSHOT")
	if out == "" {
		t.Skip("set DECKK_SCREENSHOT to write the README screenshot")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)

	now := time.Now()
	dir := t.TempDir()
	at := func(d time.Duration) time.Time { return now.Add(-d) }
	sessions := []claude.Session{
		{ID: "a1", Title: "Checkout flow redesign", Cwd: dir + "/shop", Branch: "checkout", PID: 1, TTY: "/dev/t1", Status: "idle", StatusAt: at(4 * time.Minute), UpdatedAt: at(4 * time.Minute),
			LastPrompt: "make the payment step work on small screens", LastReply: "The payment form now stacks on phones and the totals stay visible while you scroll.", PR: "#214", PRURL: "https://github.com/acme/shop/pull/214"},
		{ID: "a2", Title: "Search API pagination", Cwd: dir + "/api", Branch: "search-cursor", PID: 2, TTY: "/dev/t2", Status: "busy", StatusAt: at(3 * time.Minute), UpdatedAt: now},
		{ID: "a3", Title: "Onboarding copy pass", Cwd: dir + "/site", Branch: "main", PID: 3, TTY: "/dev/t3", Status: "idle", StatusAt: at(25 * time.Minute), UpdatedAt: at(25 * time.Minute)},
		{ID: "a4", Title: "Flaky upload test", Cwd: dir + "/api", Branch: "main", PID: 4, TTY: "/dev/t4", Status: "busy", StatusAt: at(12 * time.Minute), UpdatedAt: now},
		{ID: "a5", Title: "Release notes draft", Cwd: dir + "/docs", Branch: "main", UpdatedAt: at(40 * time.Minute)},
		{ID: "a6", Title: "Dark mode toggle", Cwd: dir + "/site", Branch: "dark-mode", UpdatedAt: at(3 * time.Hour)},
		{ID: "a7", Title: "Image resize worker", Cwd: dir + "/api", Branch: "main", UpdatedAt: at(26 * time.Hour)},
		{ID: "a8", Title: "Invoice PDF layout", Cwd: dir + "/shop", Branch: "invoices", UpdatedAt: at(50 * time.Hour)},
	}
	for i := range sessions {
		sessions[i].Dir = sessions[i].Cwd
	}
	windows := []tmux.Window{
		{ID: "@0", TTY: "/dev/t0", Dashboard: true, Session: "deckk", Active: true},
		{ID: "@1", TTY: "/dev/t1", Session: "deckk"},
		{ID: "@2", TTY: "/dev/t2", Session: "deckk"},
		{ID: "@3", TTY: "/dev/t3", Session: "deckk"},
		{ID: "@4", TTY: "/dev/t4", Session: "deckk-4", ClientTTY: "/dev/t9"},
	}

	st, _ := store.Open(filepath.Join(dir, "state.json"))
	_ = st.TogglePin("a3")
	_ = st.SetClosed([]string{"a5"}, nil, at(40*time.Minute), 24*time.Hour)
	cfg, _ := config.Load(filepath.Join(dir, "config.toml"))
	m := New("", time.Time{}, st, cfg, &fakeHost{windows: windows})
	m.width, m.height = 104, 26
	m.loaded, m.startedAt = true, time.Time{}
	m.sessions, m.tabs = sessions, windows
	m.windows, m.popped = byTTY(windows)
	m.lastSeen = map[string]time.Time{"a1": at(time.Hour), "a3": now}
	m.rearrange()

	screen := strip(m.width) + "\n\n" + m.View()
	if err := os.WriteFile(out, []byte(screen), 0o644); err != nil {
		t.Fatal(err)
	}
}

// strip draws the tmux tab strip the way tmux/config.go styles it.
func strip(width int) string {
	tab := lipgloss.NewStyle().Background(lipgloss.Color("#30304A")).Foreground(lipgloss.Color("#B4B4C0"))
	active := lipgloss.NewStyle().Background(lipgloss.Color("#E8875B")).Foreground(lipgloss.Color("#1E1E2E")).Bold(true)
	glyph := func(c, g string) string {
		return lipgloss.NewStyle().Background(lipgloss.Color("#30304A")).Foreground(lipgloss.Color(c)).Render(g)
	}
	tabs := []string{
		active.Render("  ◆ deckk  "),
		tab.Render("  ") + glyph("#E8875B", "●") + lipgloss.NewStyle().Background(lipgloss.Color("#30304A")).Foreground(lipgloss.Color("#E8875B")).Bold(true).Render(" Checkout flow redesign  "),
		tab.Render("  ") + glyph("#7AA2F7", "◓") + tab.Render(" Search API pagination  "),
		tab.Render("  ") + glyph("#E8875B", "●") + tab.Render(" Onboarding copy pass  "),
	}
	left := " " + strings.Join(tabs, " ")
	right := ghost.Render("⌃Space dashboard   ") + lipgloss.NewStyle().Background(lipgloss.Color("#30304A")).Foreground(lipgloss.Color("#E8875B")).Bold(true).Render("  + new  ") + " "
	return left + strings.Repeat(" ", max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)) + right
}
