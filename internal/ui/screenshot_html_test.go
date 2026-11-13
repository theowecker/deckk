package ui

import (
	"fmt"
	"html"
	"strconv"
	"strings"
)

const (
	cellW = 8.4
	cellH = 19.0
)

type pen struct {
	fg, bg                         string
	bold, faint, italic, underline bool
}

// Fonts draw block glyphs at their own widths and heights, so those become
// boxes on a fixed cell grid.
func screenshotHTML(ansiText string, cols int) string {
	var cells strings.Builder
	lines := strings.Split(ansiText, "\n")
	for row, line := range lines {
		var p pen
		col := 0
		for i := 0; i < len(line); {
			if strings.HasPrefix(line[i:], "\x1b[") {
				end := strings.IndexByte(line[i:], 'm')
				p = p.apply(line[i+2 : i+end])
				i += end + 1
				continue
			}
			r, size := decodeRune(line[i:])
			i += size
			cells.WriteString(p.cell(r, col, row))
			col++
		}
	}
	width, height := float64(cols)*cellW, float64(len(lines))*cellH
	return fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8">
<link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:ital,wght@0,400;0,700;1,400&display=block" rel="stylesheet">
<style>
body { margin: 0; background: transparent; }
.window { display: inline-block; background: #232338; border-radius: 10px; padding: 44px 32px 24px; position: relative; }
.dots { position: absolute; top: 14px; left: 14px; display: flex; gap: 8px; }
.dots i { width: 12px; height: 12px; border-radius: 50%%; display: block; }
.grid { position: relative; width: %.1fpx; height: %.1fpx; font: 14px/%.0fpx "JetBrains Mono", Menlo, monospace; color: #C0C0D0; }
.grid span { position: absolute; white-space: pre; height: %.0fpx; }
</style></head><body><div class="window"><div class="dots"><i style="background:#FF5F57"></i><i style="background:#FEBC2E"></i><i style="background:#28C840"></i></div>
<div class="grid">%s</div></div></body></html>`, width, height, cellH, cellH, cells.String())
}

func decodeRune(s string) (rune, int) {
	for i, r := range s {
		if i == 0 {
			return r, len(string(r))
		}
	}
	return 0, 1
}

func (p pen) apply(params string) pen {
	codes := strings.Split(params, ";")
	for i := 0; i < len(codes); i++ {
		switch codes[i] {
		case "", "0":
			p = pen{}
		case "1":
			p.bold = true
		case "2":
			p.faint = true
		case "3":
			p.italic = true
		case "4":
			p.underline = true
		case "22":
			p.bold, p.faint = false, false
		case "23":
			p.italic = false
		case "24":
			p.underline = false
		case "39":
			p.fg = ""
		case "49":
			p.bg = ""
		case "38", "48":
			if i+4 < len(codes) && codes[i+1] == "2" {
				color := rgb(codes[i+2 : i+5])
				if codes[i] == "38" {
					p.fg = color
				} else {
					p.bg = color
				}
				i += 4
			}
		}
	}
	return p
}

func rgb(parts []string) string {
	var c [3]int
	for i, s := range parts {
		c[i], _ = strconv.Atoi(s)
	}
	return fmt.Sprintf("#%02X%02X%02X", c[0], c[1], c[2])
}

func (p pen) cell(r rune, col, row int) string {
	x, y := float64(col)*cellW, float64(row)*cellH
	box := func(top, h float64, color string) string {
		return fmt.Sprintf(`<span style="left:%.1fpx;top:%.1fpx;width:%.1fpx;height:%.1fpx;background:%s"></span>`, x, y+top, cellW+0.3, h+0.3, color)
	}
	fg := p.fg
	if fg == "" {
		fg = "#C0C0D0"
	}
	var out string
	if p.bg != "" {
		out = box(0, cellH, p.bg)
	}
	switch r {
	case ' ':
		return out
	case '█':
		return out + box(0, cellH, fg)
	case '▀':
		return out + box(0, cellH/2, fg)
	case '▄':
		return out + box(cellH/2, cellH/2, fg)
	}
	style := "left:" + fmt.Sprintf("%.1fpx", x) + ";top:" + fmt.Sprintf("%.1fpx", y) + ";color:" + fg
	if p.bold {
		style += ";font-weight:700"
	}
	if p.faint {
		style += ";opacity:.6"
	}
	if p.italic {
		style += ";font-style:italic"
	}
	if p.underline {
		style += ";text-decoration:underline"
	}
	return out + `<span style="` + style + `">` + html.EscapeString(string(r)) + `</span>`
}
