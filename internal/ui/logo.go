package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var reels = []string{"◐", "◓", "◑", "◒"}

const reelEvery = 6

func reel(frame int) string { return reels[frame/3%len(reels)] }

// Every line must stay exactly 32 cells wide or the tape won't center.
func cassette(frame int) string {
	r := accent.Render(reel(frame))
	body := func(s string) string { return ghost.Render(s) }
	lines := []string{
		body("╭──────────────────────────────╮"),
		body("│  ╭────────────────────────╮  │"),
		body("│  │  ") + brand.Render("deckk") + dim.Render("         side A") + body("  │  │"),
		body("│  │    ") + r + body("  ") + dim.Render("══════════") + body("  ") + r + body("    │  │"),
		body("│  ╰────────────────────────╯  │"),
		body("│      ╱────────────────╲      │"),
		body("╰─────○──────────────────○─────╯"),
	}
	return strings.Join(lines, "\n")
}

func miniDeck(frame int, spinning bool) []string {
	left, right, reelStyle := "◐", "◐", accent
	if spinning {
		left, right, reelStyle = reel(frame), reel(frame+6), working
	}
	r := func(g string) string { return ghost.Render("(") + reelStyle.Render(g) + ghost.Render(")") }
	return []string{
		ghost.Render("┌─ ") + brand.Render("deckk") + ghost.Render(" ──────┐"),
		ghost.Render("│ ") + r(left) + dim.Render(" ════ ") + r(right) + ghost.Render(" │"),
		ghost.Render("└─╱──────────╲─┘"),
	}
}

func (m Model) splash() string {
	block := cassette(m.frame) + "\n\n" + lipgloss.PlaceHorizontal(32, lipgloss.Center, dim.Render("rewinding your sessions…"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, block)
}
