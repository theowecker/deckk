package ui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/theowecker/deckk/internal/claude"
)

func (m Model) startRename(s claude.Session) (tea.Model, tea.Cmd) {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "leave empty to use Claude's title"
	ti.Cursor.Style = accent
	ti.CharLimit = 80
	if _, custom := m.store.Names[s.ID]; custom || s.Title != "" {
		ti.SetValue(s.Title)
	}
	m.renaming, m.renameID = &ti, s.ID
	return m, ti.Focus()
}

func (m Model) updateRename(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.renaming = nil
		return m, nil
	case "enter":
		name := m.renaming.Value()
		m.renaming = nil
		if err := m.store.Rename(m.renameID, name); err != nil {
			m.setFlash("", err)
			return m, nil
		}
		// Clearing a name needs Claude's title back, which only a reload has.
		if m.store.Names[m.renameID] == "" {
			return m, m.load(false)
		}
		m.store.ApplyNames(m.sessions)
		m.rearrange()
		return m, m.syncLabels()
	}
	var cmd tea.Cmd
	*m.renaming, cmd = m.renaming.Update(msg)
	return m, cmd
}
