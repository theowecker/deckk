package hq

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/theowecker/deckk/internal/config"
)

// Claude Code's trust question is unsettling with no context, so this explains
// the deckk session first. Saying no turns it off.
func Intro(in io.Reader, out io.Writer, cfg *config.Config) bool {
	clay := lipgloss.NewStyle().Foreground(lipgloss.Color("#E8875B")).Bold(true)
	text := lipgloss.NewStyle().Foreground(lipgloss.Color("#ECECEC"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#9A9AAA"))
	key := lipgloss.NewStyle().Foreground(lipgloss.Color("#ECECEC")).Bold(true)
	width := 64
	para := func(s string) string { return text.Width(width).Render(s) + "\n\n" }

	var b strings.Builder
	b.WriteString("\n" + clay.Render("The deckk session") + "\n\n")
	b.WriteString(para("A Claude that watches your other sessions, keeps your goals and suggests what to do next. Its suggestions are typed into a session's prompt for you to send; it never sends them for you unless you allow it in settings."))
	folder := Dir()
	if home, err := os.UserHomeDir(); err == nil {
		if rest, ok := strings.CutPrefix(folder, home); ok {
			folder = "~" + rest
		}
	}
	b.WriteString(para("It runs in " + folder + ", a folder deckk made for it. The folder holds only its instructions, its notes and your goals."))
	b.WriteString(para("Next, Claude Code asks whether you trust that folder. It asks this for any folder it hasn't seen. Choose \"Yes, I trust this folder\" to start the deckk session."))
	b.WriteString(dim.Render("Press ") + key.Render("Enter") + dim.Render(" to go on, or ") + key.Render("n") + dim.Render(" then Enter to turn the deckk session off.") + "\n")
	fmt.Fprint(out, b.String())

	answer, _ := bufio.NewReader(in).ReadString('\n')
	if strings.EqualFold(strings.TrimSpace(answer), "n") {
		cfg.HQ = config.Off
		if err := cfg.Save(); err != nil {
			fmt.Fprintln(out, err)
		}
		fmt.Fprintln(out, dim.Render("The deckk session is off. Turn it back on in deckk's settings, s."))
		return false
	}
	return true
}
