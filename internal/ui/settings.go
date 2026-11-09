package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/theowecker/deckk/internal/config"
)

func (m Model) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "s", "ctrl+c":
		m.settings = false
		m.scroll()
	case "up", "k":
		m.moveActive(-1)
	case "down", "j":
		m.moveActive(1)
	case "enter", " ", "right", "l":
		if err := m.cfg.Cycle(config.Options[m.setting]); err != nil {
			m.setFlash("", err)
		}
	}
	return m, nil
}

func (m Model) settingsBody() ([]string, []int) {
	lines := []string{pad() + ghost.Render("FOR THE CLAUDE SESSIONS DECKK STARTS OR RESUMES"), ""}
	owners := []int{noOwner, noOwner}
	for i, o := range config.Options {
		bar, label := "  ", text
		if i == m.setting {
			bar, label = accent.Render("▌ "), bold
		}
		value := ghost.Render("Claude's default")
		if v := m.cfg.Value(o); v != "" {
			value = accent.Render(v)
		}
		lines = append(lines, bar+label.Render(fmt.Sprintf("%-14s", o.Label))+value)
		owners = append(owners, i)
	}
	for _, note := range []string{
		"",
		"Changes apply to sessions started or resumed from now on.",
		"Anything else, like extra claude flags, goes in " + abbreviateHome(m.cfg.Path()),
	} {
		lines = append(lines, pad()+dim.Render(note))
		owners = append(owners, noOwner)
	}
	return lines, owners
}
