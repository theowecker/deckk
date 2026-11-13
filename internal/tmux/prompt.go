package tmux

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Claude Code draws its input as a ❯ line between two rules. When nothing is
// typed it may show a suggestion of its own there in dim text, which typing
// replaces, so dim text counts as empty.
const promptMark = "❯"

// promptText reads a capture made with escapes and returns what the user has
// typed. found is false when no prompt is showing, such as while a permission
// question is up.
func promptText(capture string) (typed string, found bool) {
	lines := strings.Split(capture, "\n")
	start := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimLeft(ansi.Strip(lines[i]), " "), promptMark) {
			start = i
			break
		}
	}
	if start < 0 {
		return "", false
	}
	// A long prompt wraps onto more lines before the rule under it. Menus,
	// like the one for slash commands, take the rule's place, and then only
	// the ❯ line is the prompt.
	end := start + 1
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(ansi.Strip(lines[i])), "──") {
			end = i
			break
		}
	}
	var words []string
	for i := start; i < end; i++ {
		words = append(words, typedText(lines[i], i == start)...)
	}
	return strings.Join(words, " "), true
}

// typedText returns a line's visible words that aren't dim.
func typedText(line string, first bool) []string {
	var b strings.Builder
	dim := false
	seenMark := !first
	p := ansi.NewParser()
	state := byte(0)
	for len(line) > 0 {
		seq, width, n, newState := ansi.DecodeSequence(line, state, p)
		state = newState
		switch {
		case width > 0:
			text := strings.ReplaceAll(seq, "\u00a0", " ")
			switch {
			case !seenMark && strings.TrimSpace(text) == promptMark:
				seenMark = true
			case !seenMark:
			case dim:
				b.WriteString(" ")
			default:
				b.WriteString(text)
			}
		case strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m"):
			dim = applyDim(dim, seq)
		}
		line = line[n:]
	}
	return strings.Fields(b.String())
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

// A Delivery says what Deliver did, or why it held back.
type Delivery int

const (
	Delivered Delivery = iota
	// Busy means the user has typed something of their own.
	Busy
	// NoPrompt means a menu or question is up instead of the prompt.
	NoPrompt
	// Scrolled means the pane is in copy mode, where keys are commands.
	Scrolled
)

// Claude Code reads keys that arrive together as a paste, which turns a
// trailing Enter into a newline, so Enter waits a moment.
const enterAfter = 200 * time.Millisecond

// Deliver types text into the Claude prompt in a pane, given by pane ID or by
// its tty. It only types into an empty prompt, or one still holding exactly
// replace, which it clears first. send presses Enter after.
func Deliver(target, text, replace string, send bool) (Delivery, error) {
	if strings.HasPrefix(target, "/dev/") {
		pane, err := paneByTTY(target)
		if err != nil {
			return 0, err
		}
		target = pane
	}
	mode, err := run("display-message", "-p", "-t", target, "#{pane_in_mode}")
	if err != nil {
		return 0, err
	}
	if mode != "0" {
		return Scrolled, nil
	}
	capture, err := run("capture-pane", "-p", "-e", "-t", target)
	if err != nil {
		return 0, err
	}
	typed, found := promptText(capture)
	switch {
	case !found:
		return NoPrompt, nil
	case typed != "" && typed != oneLine(replace):
		return Busy, nil
	case typed != "":
		if _, err := run("send-keys", "-t", target, "C-u"); err != nil {
			return 0, err
		}
	}
	if text = oneLine(text); text != "" {
		if _, err := run("send-keys", "-t", target, "-l", "--", tmuxArg(text)); err != nil {
			return 0, err
		}
	}
	if send && text != "" {
		time.Sleep(enterAfter)
		if _, err := run("send-keys", "-t", target, "Enter"); err != nil {
			return 0, err
		}
	}
	return Delivered, nil
}

// Newlines would submit early, so text always goes in as one line.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// tmux reads an argument ending in ; as the end of a command, even inside
// send-keys -l, unless the ; is escaped.
func tmuxArg(s string) string {
	if strings.HasSuffix(s, ";") {
		return s[:len(s)-1] + `\;`
	}
	return s
}

func paneByTTY(tty string) (string, error) {
	out, err := run("list-panes", "-a", "-F", "#{pane_id}\t#{pane_tty}")
	if err != nil {
		return "", err
	}
	for line := range strings.Lines(out) {
		if id, paneTTY, ok := strings.Cut(strings.TrimSpace(line), "\t"); ok && paneTTY == tty {
			return id, nil
		}
	}
	return "", fmt.Errorf("no pane is on %s", tty)
}
