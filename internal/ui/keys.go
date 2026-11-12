package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.flash = ""
	key := msg.String()
	confirm := m.confirm
	m.confirm = ""
	switch key {
	case "q", "ctrl+c":
		return m, m.detach()
	case "up", "k", "ctrl+p":
		m.move(-1)
	case "down", "j", "ctrl+n":
		m.move(1)
	case "g", "home":
		m.move(-len(m.flat))
	case "G", "end":
		m.move(len(m.flat))
	case "/", "f":
		return m.switchView(findView)
	case "tab":
		return m.switchView(m.view.next())
	case "shift+tab":
		return m.switchView(m.view.prev())
	case "+", "=":
		m.showAll = !m.showAll
		m.rearrange()
	case "v":
		m.byProject = !m.byProject
		m.offset = 0
		m.rearrange()
	case "r":
		if s, ok := m.selected(); ok {
			return m.startRename(s)
		}
	case "n":
		return m.openPicker()
	case "s":
		m.settings, m.offset = true, 0
	case "enter":
		if s, ok := m.selected(); ok {
			return m, m.open(s)
		}
	case "x":
		if s, ok := m.selected(); ok {
			return m.close(s, confirm)
		}
	case "m":
		if s, ok := m.selected(); ok {
			return m.adopt(s, confirm)
		}
	case "M":
		return m, m.adoptAll()
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		return m, m.switchToTab(int(key[0] - '0'))
	case "O":
		return m, m.popAll()
	case "o":
		if s, ok := m.selected(); ok {
			return m, m.popOut(s)
		}
	case "p":
		if s, ok := m.selected(); ok {
			cmd := m.togglePin(s)
			return m, cmd
		}
	case "a":
		if s, ok := m.selected(); ok {
			m.mark(m.store.ToggleArchive(s.ID))
		}
	case "R":
		return m, m.restoreWorkspace()
	case "?":
		m.help = true
	}
	return m, nil
}

func (m *Model) mark(err error) {
	if err != nil {
		m.setFlash("", err)
	}
	m.rearrange()
}

func keyMsg(key string) tea.KeyMsg {
	switch key {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "↑↓":
		return tea.KeyMsg{Type: tea.KeyDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}
