package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.flash = ""
	key := msg.String()
	if m.typing(msg) {
		m.setFlash(typingHint(m.deckk.open), nil)
		return m, nil
	}
	confirm := m.confirm
	m.confirm = ""
	if s, ok := m.selected(); ok && isProposal(s) && strings.Contains("xompra", key) && len(key) == 1 {
		m.setFlash("That session doesn't exist yet. Press ↵ to start it or d to dismiss it", nil)
		return m, nil
	}
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
		if m.view == boardView {
			break
		}
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
			cmd := m.openWithSuggestion(s)
			return m, cmd
		}
	case "c":
		cmd := m.openDeckk()
		return m, cmd
	case "w":
		return m, m.openWeb()
	case "d":
		if s, ok := m.selected(); ok {
			cmd := m.dismiss(s)
			return m, cmd
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
		cmd := m.confirmAdoptAll(confirm)
		return m, cmd
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		return m, m.switchToTab(int(key[0] - '0'))
	case "O":
		cmd := m.confirmPopAll(confirm)
		return m, cmd
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

// Shortcuts are single keys pressed one at a time. Letters arriving faster
// than this are someone typing, most likely meant for the deckk session.
const typingGap = 150 * time.Millisecond

// typing reports a letter that follows another too closely to be a shortcut.
func (m *Model) typing(msg tea.KeyMsg) bool {
	if msg.Type != tea.KeyRunes || msg.Alt || len(msg.Runes) != 1 || m.typingGap == 0 {
		return false
	}
	now := time.Now()
	burst := now.Sub(m.lastLetter) < m.typingGap
	m.lastLetter = now
	return burst
}

func typingHint(deckkOpen bool) string {
	if deckkOpen {
		return "Looks like you're typing. Shortcuts are paused; press c to talk to the deckk session"
	}
	return "Looks like you're typing. Shortcuts are single keys, so these were ignored"
}
