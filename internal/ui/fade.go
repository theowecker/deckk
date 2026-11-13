package ui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// fadeBy is how far every colour moves toward grey while the dashboard is
// faded. Backgrounds move too but keep their shape, so the cassette
// stays a cassette.
const fadeBy = 0.6

var sgr = regexp.MustCompile(`\x1b\[([0-9;:]*)m`)

// fade redraws a rendered view in muted colours while the deckk session beside
// it has the keyboard. Text without a colour of its own gets the grey too.
func fade(view string) string {
	grey := [3]int{0x5E, 0x5E, 0x72}
	if !lipgloss.HasDarkBackground() {
		grey = [3]int{0xA8, 0xA8, 0xB0}
	}
	base := fmt.Sprintf("38;2;%d;%d;%d", grey[0], grey[1], grey[2])
	faded := sgr.ReplaceAllStringFunc(view, func(seq string) string {
		return "\x1b[" + fadeParams(sgr.FindStringSubmatch(seq)[1], grey, base) + "m"
	})
	return "\x1b[" + base + "m" + faded
}

// fadeParams mutes the colours in one SGR sequence and puts the grey back
// after anything that resets the foreground.
func fadeParams(params string, grey [3]int, base string) string {
	codes := strings.Split(params, ";")
	var out []string
	for i := 0; i < len(codes); i++ {
		switch codes[i] {
		case "", "0", "39":
			out = append(out, codes[i], base)
		case "38", "48":
			if i+4 < len(codes) && codes[i+1] == "2" {
				var c [3]int
				for j := range c {
					v, _ := strconv.Atoi(codes[i+2+j])
					c[j] = int(float64(v)*(1-fadeBy) + float64(grey[j])*fadeBy)
				}
				out = append(out, codes[i], "2", strconv.Itoa(c[0]), strconv.Itoa(c[1]), strconv.Itoa(c[2]))
				i += 4
				continue
			}
			out = append(out, codes[i])
		default:
			out = append(out, codes[i])
		}
	}
	return strings.Join(out, ";")
}
