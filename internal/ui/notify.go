package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/hq"
)

const notifyEvery = time.Minute

func (m *Model) notify(events []hq.Event) tea.Cmd {
	if !m.cfg.NotifyOn() {
		return nil
	}
	now := time.Now()
	var cmds []tea.Cmd
	for _, e := range events {
		pr := e.Kind == hq.ChecksFailed || e.Kind == hq.ChangesRequested
		if e.Kind != hq.Finished && !pr || now.Sub(m.notified[e.Session]) < notifyEvery {
			continue
		}
		s, ok := m.sessionByID(e.Session)
		if !ok || m.watching(s) {
			continue
		}
		m.notified[e.Session] = now
		title, body := displayTitle(s)+" is waiting", e.Detail
		if pr {
			title = displayTitle(s) + e.Kind
		}
		if body == "" {
			body = "Claude finished in " + s.Project()
		}
		cmds = append(cmds, func() tea.Msg {
			if err := m.host.Notify(s.ID, title, s.Project(), body); err != nil {
				return flashMsg{err: err}
			}
			return nil
		})
	}
	return tea.Batch(cmds...)
}

func (m Model) watching(s claude.Session) bool {
	for _, w := range m.tabs {
		if w.TTY == s.TTY && s.TTY != "" {
			return w.Active
		}
	}
	return false
}
