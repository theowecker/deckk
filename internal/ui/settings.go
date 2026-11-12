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
		return m, m.cycleSetting()
	}
	return m, nil
}

func (m *Model) cycleSetting() tea.Cmd {
	o := config.Options[m.setting]
	if err := m.cfg.Cycle(o); err != nil {
		m.setFlash("", err)
		return nil
	}
	if o.Key != "tab_keys" {
		return nil
	}
	preset := m.cfg.Value(o)
	return func() tea.Msg {
		if err := m.host.SetTabKeys(preset); err != nil {
			return flashMsg{err: err}
		}
		return nil
	}
}

func (m Model) settingsBody() ([]string, []int) {
	var lines []string
	var owners []int
	heading := func(title string) {
		if len(lines) > 0 {
			lines, owners = append(lines, ""), append(owners, noOwner)
		}
		lines = append(lines, pad()+ghost.Render(title), "")
		owners = append(owners, noOwner, noOwner)
	}
	heading("FOR THE CLAUDE SESSIONS DECKK STARTS OR RESUMES")
	for i, o := range config.Options {
		if o.Deckk && (i == 0 || !config.Options[i-1].Deckk) {
			heading("DECKK")
		}
		bar, label := "  ", text
		if i == m.setting {
			bar, label = accent.Render("▌ "), bold
		}
		v := m.cfg.Value(o)
		value := ghost.Render("Claude's default")
		if name, ok := o.Names[v]; ok {
			value = accent.Render(name)
		} else if v != "" {
			value = accent.Render(v)
		}
		lines = append(lines, bar+label.Render(fmt.Sprintf("%-14s", o.Label))+value)
		owners = append(owners, i)
	}
	for _, note := range []string{
		"",
		"Claude settings apply to sessions started or resumed from now on.",
		"Anything else, like extra claude flags, goes in " + abbreviateHome(m.cfg.Path()),
	} {
		lines = append(lines, pad()+dim.Render(note))
		owners = append(owners, noOwner)
	}
	return lines, owners
}
