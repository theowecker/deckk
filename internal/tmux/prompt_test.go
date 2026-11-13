package tmux

import "testing"

// Lines as tmux capture-pane -e returned them from Claude Code.
const (
	rule       = "\x1b[38;5;244m────────────────────────────────\n"
	statusLine = "\x1b[39m  \x1b[38;5;220m⏵⏵\x1b[39m \x1b[38;5;220mauto\x1b[39m \x1b[38;5;220mmode\x1b[39m\n"
)

func TestPromptIsEmpty(t *testing.T) {
	cases := []struct {
		name         string
		capture      string
		empty, found bool
	}{
		{"empty", "reply text\n" + rule + "\x1b[38;5;246m❯ \x1b[39m\n" + rule + statusLine, true, true},
		{"claude's own dim suggestion", rule + "\x1b[39m❯ \x1b[2mok it's plugged in, install it\x1b[0m\n" + rule + statusLine, true, true},
		{"dim words one by one", rule + "\x1b[39m❯ \x1b[2mmatch\x1b[0m \x1b[2mthe\x1b[0m \x1b[2mring\x1b[0m\n" + rule, true, true},
		{"typed", rule + "\x1b[39m❯ fix the cart total\n" + rule + statusLine, false, true},
		{"typed on a second line", rule + "\x1b[39m❯ \n  and the tax\n" + rule, false, true},
		{"a permission question", "Do you want to proceed?\n❯ 1. Yes\n", false, true},
		{"no prompt", "just output\n", false, false},
	}
	for _, c := range cases {
		empty, found := promptIsEmpty(c.capture)
		if empty != c.empty || found != c.found {
			t.Errorf("%s: empty %v found %v, want %v %v", c.name, empty, found, c.empty, c.found)
		}
	}
}
