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
		case "38", "48", "58":
			code := codes[i]
			var rgb [3]int
			switch {
			case i+4 < len(codes) && codes[i+1] == "2":
				for j := range rgb {
					rgb[j], _ = strconv.Atoi(codes[i+2+j])
				}
				i += 4
			case i+2 < len(codes) && codes[i+1] == "5":
				n, _ := strconv.Atoi(codes[i+2])
				rgb = xterm256(n)
				i += 2
			default:
				out = append(out, codes[i])
				continue
			}
			var c [3]int
			for j := range c {
				c[j] = int(float64(rgb[j])*(1-fadeBy) + float64(grey[j])*fadeBy)
			}
			out = append(out, code, "2", strconv.Itoa(c[0]), strconv.Itoa(c[1]), strconv.Itoa(c[2]))
		default:
			out = append(out, codes[i])
		}
	}
	return strings.Join(out, ";")
}

// xterm256 is the colour a 256-colour index stands for in xterm's palette.
func xterm256(n int) [3]int {
	basic := [16][3]int{{0, 0, 0}, {205, 0, 0}, {0, 205, 0}, {205, 205, 0}, {0, 0, 238}, {205, 0, 205}, {0, 205, 205}, {229, 229, 229},
		{127, 127, 127}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0}, {92, 92, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255}}
	switch {
	case n < 16:
		return basic[max(n, 0)]
	case n < 232:
		n -= 16
		level := func(v int) int {
			if v == 0 {
				return 0
			}
			return 55 + v*40
		}
		return [3]int{level(n / 36), level(n / 6 % 6), level(n % 6)}
	default:
		v := 8 + (min(n, 255)-232)*10
		return [3]int{v, v, v}
	}
}
