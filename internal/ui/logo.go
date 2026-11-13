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
	shade = lipgloss.AdaptiveColor{Light: "#A04524", Dark: "#C96B45"}

	palette = map[rune]lipgloss.TerminalColor{'C': clay, 'c': shade, 'L': cream, 'D': ink}
	sticker = lipgloss.NewStyle().Background(cream).Foreground(ink)
	window  = lipgloss.NewStyle().Background(ink).Foreground(cream)
	film    = lipgloss.NewStyle().Background(ink).Foreground(lipgloss.Color("#6B5A50"))
)

// Two bitmap rows per line of half blocks, so each cell is two square pixels. A
// dot is transparent.
func pixels(top, bottom string) string {
	t, b := []rune(top), []rune(bottom)
	var line strings.Builder
	for i := range t {
		up, down := palette[t[i]], palette[b[i]]
		switch {
		case up == nil && down == nil:
			line.WriteString(" ")
		case up == nil:
			line.WriteString(lipgloss.NewStyle().Foreground(down).Render("▄"))
		case down == nil || up == down:
			glyph := "▀"
			if up == down {
				glyph = "█"
			}
			line.WriteString(lipgloss.NewStyle().Foreground(up).Render(glyph))
		default:
			line.WriteString(lipgloss.NewStyle().Foreground(up).Background(down).Render("▀"))
		}
	}
	return line.String()
}

func solid(code rune, n int) string {
	return lipgloss.NewStyle().Background(palette[code]).Render(strings.Repeat(" ", n))
}

func tapeWindow(left, right string, width int) string {
	tape := width - 6
	return window.Render(" "+left+" ") + film.Render(strings.Repeat("━", tape)) + window.Render(" "+right+" ")
}

// Every line is 32 cells wide so the splash centers.
func cassette(frame int) string {
	r := reel(frame)
	label := sticker.Bold(true).Render(" deckk") + sticker.Render(strings.Repeat(" ", 15)) + sticker.Faint(true).Render("side A ")
	return strings.Join([]string{
		pixels(".CCCCCCCCCCCCCCCCCCCCCCCCCCCCCC.", "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC"),
		solid('C', 2) + label + solid('C', 2),
		solid('C', 2) + solid('L', 3) + tapeWindow(r, r, 22) + solid('L', 3) + solid('C', 2),
		pixels("CCLLLLLLLLLLLLLLLLLLLLLLLLLLLLCC", "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC"),
		pixels("CCCCCCCCcccccccccccccccccCCCCCCC", ".CCCCCcccccccccccccccccccccCCCC."),
	}, "\n")
}

func miniDeck(frame int, spinning bool) []string {
	left, right := "◐", "◐"
	if spinning {
		left, right = reel(frame), reel(frame+6)
	}
	pad := strings.Repeat(" ", gutter)
	return []string{
		pixels(".CCCCCCCCCCCCC.", "CCLLLLLLLLLLLCC") + pad + "     ",
		solid('C', 2) + tapeWindow(left, right, 11) + solid('C', 2) + pad + brand.Render("deckk"),
		pixels("CCCcccccccccCCC", ".CcccccccccccC.") + pad + "     ",
	}
}

func (m Model) splash() string {
	block := cassette(m.frame) + "\n\n" + lipgloss.PlaceHorizontal(32, lipgloss.Center, dim.Render("rewinding your sessions…"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, block)
}
