package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/tmux"
)

type pinTabMsg string

func pinnedName(s claude.Session) string {
	first, _, _ := strings.Cut(strings.TrimSpace(displayTitle(s)), " ")
	return fitName(first, 8)
}

// Asks tmux rather than trusting m.tabs, which can be a refresh behind.
func (m Model) currentTab() tea.Cmd {
	return m.activeTab(func(tty string) tea.Msg { return pinTabMsg(tty) })
}

func (m Model) activeTab(msg func(tty string) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		windows, err := m.host.Windows()
		if err != nil {
			return flashMsg{err: err}
		}
		for _, w := range windows {
			if w.Session == tmux.Session && w.Active && !w.Dashboard {
				return msg(w.TTY)
			}
		}
		return nil
	}
}

func (m *Model) pinTab(tty string) tea.Cmd {
	for _, s := range m.sessions {
		if s.Running() && s.TTY == tty {
			return m.togglePin(s)
		}
	}
	return nil
}

func (m *Model) togglePin(s claude.Session) tea.Cmd {
	if err := m.store.TogglePin(s.ID); err != nil {
		m.mark(err)
		return nil
	}
	m.mark(nil)
	id, hosted := m.window(s)
	if !hosted {
		return nil
	}
	after := ""
	for _, w := range m.tabs {
		if w.Session != tmux.Session || w.PoppedOut() || w.ID == id {
			continue
		}
		if w.Dashboard || m.pinnedTab(w) {
			after = w.ID
		}
	}
	if after == "" {
		return m.syncLabels()
	}
	return tea.Batch(m.syncLabels(), func() tea.Msg {
		if err := m.host.MoveAfter(id, after); err != nil {
			return flashMsg{err: err}
		}
		return flashMsg{}
	})
}

func (m Model) pinnedTab(w tmux.Window) bool {
	for _, s := range m.sessions {
		if s.Running() && s.TTY == w.TTY {
			return m.store.Pinned[s.ID]
		}
	}
	return false
}
