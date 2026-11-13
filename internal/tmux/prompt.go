package tmux

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

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

// applyDim follows the dim attribute through one SGR sequence. Colour codes
// carry numbers of their own, as in 38;2;r;g;b or 38;5;n, which are skipped
// so a 2 or 0 inside a colour isn't read as dim or reset.
func applyDim(dim bool, sgr string) bool {
	params := strings.TrimSuffix(strings.TrimPrefix(sgr, "\x1b["), "m")
	if params == "" {
		return false
	}
	codes := strings.FieldsFunc(params, func(r rune) bool { return r == ';' || r == ':' })
	for i := 0; i < len(codes); i++ {
		switch n, _ := strconv.Atoi(codes[i]); n {
		case 0, 22:
			dim = false
		case 2:
			dim = true
		case 38, 48, 58:
			if i+1 < len(codes) && codes[i+1] == "5" {
				i += 2
			} else if i+1 < len(codes) && codes[i+1] == "2" {
				i += 4
			}
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
	case typed != "" && !sameText(typed, replace):
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
	if send && text != "" && !commandLike(text) {
		time.Sleep(enterAfter)
		if _, err := run("send-keys", "-t", target, "Enter"); err != nil {
			// Left in the prompt, the text would read as the user's own
			// and block every later delivery.
			_, _ = run("send-keys", "-t", target, "C-u")
			return 0, err
		}
	}
	return Delivered, nil
}

// oneLine makes text safe to type: newlines would submit early, and control
// characters would reach Claude as keys such as Escape or Ctrl+C.
func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// A leading / or ! makes Claude run a command or a shell line, so text like
// that is typed in for the user to read but never sent on its own.
func commandLike(text string) bool {
	return strings.HasPrefix(text, "/") || strings.HasPrefix(text, "!")
}

// sameText compares a prompt as read off the screen with text deckk typed.
// Claude wraps long words at the edge, and reading the lines back adds a
// space there, so spacing is ignored.
func sameText(screen, typed string) bool {
	squash := func(s string) string { return strings.Join(strings.Fields(s), "") }
	return squash(screen) == squash(oneLine(typed))
}

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
