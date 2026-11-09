package ui

import (
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/theowecker/deckk/internal/config"
	"github.com/theowecker/deckk/internal/tmux"
)

const doubleClick = 400 * time.Millisecond

func (m Model) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Action {
	case tea.MouseActionMotion:
		return m, m.hover(msg.X, msg.Y)
	case tea.MouseActionPress:
	default:
		return m, nil
	}

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.moveActive(-1)
	case tea.MouseButtonWheelDown:
		m.moveActive(1)
	case tea.MouseButtonLeft:
		if msg.Y == m.footerY() {
			if key, ok := m.hintAt(msg.X); ok {
				return m.dispatch(keyMsg(key))
			}
			return m, nil
		}
		// The first click reflows the list, so a double-click is two quick
		// clicks on the same spot, acting on what the first one selected.
		now := time.Now()
		if msg.Y == m.lastClickY && now.Sub(m.lastClick) < doubleClick {
			m.lastClick = time.Time{}
			return m.dispatch(keyMsg("enter"))
		}
		index, ok := m.itemAt(msg.Y)
		if !ok {
			return m, nil
		}
		if m.settings {
			m.setting = index
			return m.updateSettings(keyMsg("enter"))
		}
		switch index {
		case restoreOwner:
			return m, m.restoreWorkspace()
		case popAllOwner:
			return m, m.popAll()
		case moveInOwner:
			return m, m.adoptAll()
		}
		m.lastClick, m.lastClickY = now, msg.Y
		m.moveActive(index - m.activeCursor())
	}
	return m, nil
}

func (m Model) dispatch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case m.help:
		m.help = false
		return m, nil
	case m.picker != nil:
		return m.updatePicker(msg)
	case m.settings:
		return m.updateSettings(msg)
	case m.renaming != nil:
		return m.updateRename(msg)
	case m.filtering:
		return m.updateFilter(msg)
	}
	return m.updateList(msg)
}

func (m *Model) moveActive(delta int) {
	if m.settings {
		m.setting = max(0, min(m.setting+delta, len(config.Options)-1))
		return
	}
	if m.picker != nil {
		m.movePicker(delta)
	} else {
		m.move(delta)
	}
}

func (m Model) itemAt(y int) (int, bool) {
	_, owners := m.body()
	line := y - headerLines + m.offset
	if y < headerLines || y >= headerLines+m.bodyHeight() || line >= len(owners) || owners[line] == noOwner {
		return 0, false
	}
	return owners[line], true
}

func (m *Model) hover(x, y int) tea.Cmd {
	shape := "default"
	if _, ok := m.itemAt(y); ok {
		shape = "pointer"
	} else if _, ok := m.hintAt(x); ok && y == m.footerY() {
		shape = "pointer"
	}
	if shape == m.pointer {
		return nil
	}
	m.pointer = shape
	return setPointer(shape)
}

// iTerm2 honours OSC 22 alongside mouse reporting only from 3.7. Inside tmux
// it needs a passthrough wrapper to reach iTerm2 at all.
func setPointer(shape string) tea.Cmd {
	return func() tea.Msg {
		if !tmux.Inside() {
			return nil
		}
		seq := "\x1b]22;" + shape + "\x07"
		seq = "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
		_, _ = os.Stdout.WriteString(seq)
		return nil
	}
}
