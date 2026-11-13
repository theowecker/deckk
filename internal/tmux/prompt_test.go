package tmux

import "testing"

// Lines as tmux capture-pane -e returned them from Claude Code.
const (
	rule       = "\x1b[38;5;244m────────────────────────────────\n"
	statusLine = "\x1b[39m  \x1b[38;5;220m⏵⏵\x1b[39m \x1b[38;5;220mauto\x1b[39m \x1b[38;5;220mmode\x1b[39m\n"
)

func TestPromptText(t *testing.T) {
	cases := []struct {
		name    string
		capture string
		typed   string
		found   bool
	}{
		{"empty", "reply text\n" + rule + "\x1b[38;5;246m❯ \x1b[39m\n" + rule + statusLine, "", true},
		{"claude's own dim suggestion", rule + "\x1b[39m❯ \x1b[2mok it's plugged in, install it\x1b[0m\n" + rule + statusLine, "", true},
		{"dim words one by one", rule + "\x1b[39m❯ \x1b[2mmatch\x1b[0m \x1b[2mthe\x1b[0m \x1b[2mring\x1b[0m\n" + rule, "", true},
		{"typed", rule + "\x1b[39m❯ fix the cart total\n" + rule + statusLine, "fix the cart total", true},
		{"typed over two lines", rule + "\x1b[39m❯ fix the\n  cart total\n" + rule, "fix the cart total", true},
		{"a permission question", "Do you want to proceed?\n❯ 1. Yes\n", "1. Yes", true},
		{"no prompt", "just output\n", "", false},
	}
	for _, c := range cases {
		typed, found := promptText(c.capture)
		if typed != c.typed || found != c.found {
			t.Errorf("%s: %q %v, want %q %v", c.name, typed, found, c.typed, c.found)
		}
	}
}

func TestTmuxArgEscapesATrailingSemicolon(t *testing.T) {
	if got := tmuxArg("run the tests;"); got != `run the tests\;` {
		t.Fatalf("got %q", got)
	}
	if got := tmuxArg("a; b"); got != "a; b" {
		t.Fatalf("got %q", got)
	}
}

func TestColoursAreNotReadAsDim(t *testing.T) {
	typed := rule + "\x1b[39m❯\u00a0\x1b[38;2;200;200;200mfix the cart\x1b[39m\n" + rule
	if got, _ := promptText(typed); got != "fix the cart" {
		t.Fatalf("truecolour text read as %q", got)
	}
	ghost := rule + "\x1b[39m❯\u00a0\x1b[2;38;5;0mtry this\x1b[0m\n" + rule
	if got, _ := promptText(ghost); got != "" {
		t.Fatalf("dim text with a colour read as %q", got)
	}
}

func TestTextIsMadeSafeToType(t *testing.T) {
	if got := oneLine("run\x1b the\x03 tests\n now"); got != "run the tests now" {
		t.Fatalf("got %q", got)
	}
	if !commandLike("/exit") || !commandLike("!rm -rf x") || commandLike("run /tests") {
		t.Fatal("commands are the ones starting with / or !")
	}
	if !sameText("https://example.com/a-very-long- path", "https://example.com/a-very-long-path") {
		t.Fatal("a wrapped long word should still match")
	}
}
