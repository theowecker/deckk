package tmux

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Claude Code draws its input as a ❯ line between two rules. When nothing is
// typed it may show a suggestion of its own there in dim text, which typing
// replaces, so dim text counts as empty.
const promptMark = "❯"

// promptIsEmpty reads a capture made with escapes. found is false when no
// prompt is showing, such as while a permission question is up.
func promptIsEmpty(capture string) (empty, found bool) {
	lines := strings.Split(capture, "\n")
	start := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimLeft(ansi.Strip(lines[i]), " "), promptMark) {
			start = i
			break
		}
	}
	if start < 0 {
		return false, false
	}
	for i := start; i < len(lines); i++ {
		plain := ansi.Strip(lines[i])
		if i > start && strings.HasPrefix(strings.TrimSpace(plain), "──") {
			break
		}
		if typedText(lines[i], i == start) {
			return false, true
		}
	}
	return true, true
}

// typedText reports whether a line holds any visible text that isn't dim.
func typedText(line string, first bool) bool {
	dim := false
	seenMark := !first
	p := ansi.NewParser()
	state := byte(0)
	for len(line) > 0 {
		seq, width, n, newState := ansi.DecodeSequence(line, state, p)
		state = newState
		switch {
		case width > 0:
			text := strings.TrimSpace(strings.ReplaceAll(seq, " ", " "))
			switch {
			case text == "":
			case !seenMark && text == promptMark:
				seenMark = true
			case seenMark && !dim:
				return true
			}
		case strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m"):
			dim = applyDim(dim, seq)
		}
		line = line[n:]
	}
	return false
}

func applyDim(dim bool, sgr string) bool {
	params := strings.TrimSuffix(strings.TrimPrefix(sgr, "\x1b["), "m")
	if params == "" {
		return false
	}
	for code := range strings.SplitSeq(params, ";") {
		switch n, _ := strconv.Atoi(code); n {
		case 0, 22:
			dim = false
		case 2:
			dim = true
		}
	}
	return dim
}

// PromptIsEmpty looks at what a pane shows right now.
func PromptIsEmpty(target string) (bool, error) {
	out, err := run("capture-pane", "-p", "-e", "-t", target)
	if err != nil {
		return false, err
	}
	empty, found := promptIsEmpty(out)
	return empty && found, nil
}

// Type puts text in a pane's prompt as if typed. Newlines would submit early,
// so the text goes in as one line; enter submits it.
func Type(target, text string, enter bool) error {
	text = strings.Join(strings.Fields(text), " ")
	args := []string{"send-keys", "-t", target, "-l", text}
	if enter {
		args = append(args, ";", "send-keys", "-t", target, "Enter")
	}
	_, err := run(args...)
	return err
}
