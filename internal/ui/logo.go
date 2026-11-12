package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var reels = []string{"◐", "◓", "◑", "◒"}

const reelEvery = 6

func reel(frame int) string { return reels[frame/3%len(reels)] }

var (
	ink   = lipgloss.Color("#1E1E2E")
	cream = lipgloss.Color("#F4E9DC")

	shell   = lipgloss.NewStyle().Foreground(clay)
	body    = lipgloss.NewStyle().Background(clay).Foreground(ink)
	hole    = lipgloss.NewStyle().Background(ink).Foreground(clay)
	sticker = lipgloss.NewStyle().Background(cream).Foreground(ink)
)

// A cassette in solid cells, which every terminal and font draws the same.
// The tape window is dark with the reels inside, and the bottom steps down
// into the tab a real tape has.
func tape(width, tab int, rows ...string) []string {
	inset := (width - tab) / 2
	lines := []string{shell.Render(" " + strings.Repeat("▄", width-2) + " ")}
	lines = append(lines, rows...)
	return append(lines, shell.Render(" "+strings.Repeat("▀", inset-1)+strings.Repeat("█", tab)+strings.Repeat("▀", width-tab-inset-1)+" "))
}

func window(left, right, film string, side int) string {
	return hole.Render(strings.Repeat(" ", side)+left+" ") + hole.Faint(true).Render(film) + hole.Render(" "+right+strings.Repeat(" ", side))
}

// Every line is 32 cells wide so the splash centers.
func cassette(frame int) string {
	r := reel(frame)
	label := sticker.Bold(true).Render(" deckk") + sticker.Render(strings.Repeat(" ", 15)) + sticker.Faint(true).Render("side A ")
	return strings.Join(tape(32, 16,
		body.Render("  ")+label+body.Render("  "),
		body.Render("  ")+window(r, r, strings.Repeat("━", 16), 4)+body.Render("  "),
		body.Render(strings.Repeat(" ", 32)),
	), "\n")
}

// The reels only turn while a session is working.
func miniDeck(frame int, spinning bool) []string {
	left, right := "◐", "◐"
	if spinning {
		left, right = reel(frame), reel(frame+6)
	}
	pad := strings.Repeat(" ", gutter)
	lines := tape(14, 6, body.Render(" ")+window(left, right, "━━━━", 2)+body.Render(" "))
	return []string{lines[0] + pad + "     ", lines[1] + pad + brand.Render("deckk"), lines[2] + pad + "     "}
}

func (m Model) splash() string {
	block := cassette(m.frame) + "\n\n" + lipgloss.PlaceHorizontal(32, lipgloss.Center, dim.Render("rewinding your sessions…"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, block)
}
