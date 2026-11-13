package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/deck"
	"github.com/theowecker/deckk/internal/hq"
	"github.com/theowecker/deckk/internal/terminal"
	"github.com/theowecker/deckk/internal/tmux"
)

func (m Model) open(s claude.Session) tea.Cmd {
	if id, ok := m.window(s); ok {
		return leaving(m.do(func() error { return m.host.Select(id) }, ""))
	}
	if w, ok := m.poppedOut(s); ok {
		if w.ClientTTY == "" {
			return m.do(func() error { return m.host.BringBack(w.ID) }, "Its tab was closed, so it's back in deckk")
		}
		return m.do(func() error { return m.host.Focus(w.ClientTTY) }, "")
	}
	if s.Running() {
		return func() tea.Msg {
			err := m.host.Focus(s.TTY)
			if errors.Is(err, terminal.ErrNotFound) {
				err = fmt.Errorf("%s is running in a tab deckk can't switch to, on %s", displayTitle(s), s.TTY)
			}
			return flashMsg{err: err}
		}
	}
	if !m.claimResume(s.ID) {
		return func() tea.Msg { return flashMsg{text: displayTitle(s) + " is already resuming"} }
	}
	return leaving(m.do(func() error { return m.resume(s, true) }, ""))
}

func leaving(cmd tea.Cmd) tea.Cmd {
	return tea.Batch(setPointer(""), cmd)
}

// Claude only finds a conversation from the folder it started in.
func (m Model) resume(s claude.Session, focus bool) error {
	if !isDir(s.Dir) {
		return fmt.Errorf("can't resume %s: %s no longer exists", displayTitle(s), abbreviateHome(s.Dir))
	}
	return m.host.Open(s.Dir, tabName(displayTitle(s)), m.claude("--resume", s.ID), focus)
}

func (m Model) claude(args ...string) string {
	if m.cfg.AskDeckk() {
		args = append(args, hq.ChildArgs()...)
	}
	return m.cfg.Claude(args...)
}

func (m Model) newSession(dir string, create bool) tea.Cmd {
	return leaving(m.do(func() error {
		if create {
			if err := os.Mkdir(dir, 0o755); err != nil {
				return err
			}
		}
		return m.host.Open(dir, tabName(filepath.Base(dir)), m.claude(), true)
	}, ""))
}

func (m Model) close(s claude.Session, confirm string) (tea.Model, tea.Cmd) {
	id, ok := m.window(s)
	if w, popped := m.poppedOut(s); popped {
		id, ok = w.ID, true
	}
	if !ok {
		if s.Running() {
			m.setFlash("That session lives in another tab. Press m to move it into deckk", nil)
		}
		return m, nil
	}
	if deck.StateOf(s) == deck.Working && confirm != "x"+s.ID {
		m.confirm = "x" + s.ID
		m.setFlash("Claude is still working. Press x again to close it anyway", nil)
		return m, nil
	}
	m.closing[s.ID] = true
	return m, m.do(func() error { return m.host.Close(id) }, "Closed "+displayTitle(s)+". It stays resumable")
}

func (m Model) adopt(s claude.Session, confirm string) (tea.Model, tea.Cmd) {
	if w, ok := m.poppedOut(s); ok {
		return m, m.do(func() error { return m.host.BringBack(w.ID) }, "")
	}
	if !m.elsewhere(s) {
		return m, nil
	}
	if deck.StateOf(s) == deck.Working && confirm != "m"+s.ID {
		m.confirm = "m" + s.ID
		m.setFlash("Claude is still working. Moving it interrupts that turn, so press m again to move it anyway", nil)
		return m, nil
	}
	m.setFlash("Moving "+displayTitle(s)+" into deckk…", nil)
	return m, m.do(func() error { return m.moveIn(s, true) }, "")
}

func (m Model) adoptAll() tea.Cmd {
	var idle []claude.Session
	busy := 0
	var popped []tmux.Window
	for _, s := range m.sessions {
		w, isPopped := m.poppedOut(s)
		switch {
		case isPopped:
			popped = append(popped, w)
		case !m.elsewhere(s):
		case deck.StateOf(s) == deck.Working:
			busy++
		default:
			idle = append(idle, s)
		}
	}
	if len(idle) == 0 && busy == 0 && len(popped) == 0 {
		return func() tea.Msg { return flashMsg{text: "Nothing is running in other tabs"} }
	}
	text := fmt.Sprintf("Moved %d sessions into deckk", len(idle)+len(popped))
	if busy > 0 {
		text += fmt.Sprintf(". %d still working, so they stayed put", busy)
	}
	return m.do(func() error {
		for _, w := range popped {
			if err := m.host.BringBack(w.ID); err != nil {
				return err
			}
		}
		for _, s := range idle {
			if err := m.moveIn(s, false); err != nil {
				return err
			}
		}
		return nil
	}, text)
}

func (m Model) switchToTab(n int) tea.Cmd {
	position := 0
	for _, w := range m.tabs {
		if w.Dashboard || w.PoppedOut() {
			continue
		}
		if position++; position == n {
			return leaving(m.do(func() error { return m.host.Select(w.ID) }, ""))
		}
	}
	return nil
}

func (m Model) popAll() tea.Cmd {
	var ids []string
	for _, s := range m.sessions {
		if id, ok := m.window(s); ok {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return func() tea.Msg { return flashMsg{text: "Nothing is running in deckk to move out"} }
	}
	return m.do(func() error {
		for _, id := range ids {
			if err := m.host.PopOut(id); err != nil {
				return err
			}
		}
		return nil
	}, fmt.Sprintf("Gave %s its own tab. M brings them all back", sessions(len(ids))))
}

func (m Model) popOut(s claude.Session) tea.Cmd {
	id, ok := m.window(s)
	if !ok {
		return nil
	}
	return m.do(func() error { return m.host.PopOut(id) }, "")
}

func (m Model) moveIn(s claude.Session, focus bool) error {
	if !isDir(s.Dir) {
		return fmt.Errorf("can't move %s: %s no longer exists", displayTitle(s), abbreviateHome(s.Dir))
	}
	if err := m.host.Release(s.PID, s.TTY); err != nil {
		return err
	}
	return m.resume(s, focus)
}

func (m Model) elsewhere(s claude.Session) bool {
	return s.Running() && !m.inDeckk(s)
}

// A session takes a few seconds to show as running after a resume, and
// resuming it again in that time starts a second copy of the conversation.
const resumeWait = 2 * time.Minute

func (m Model) claimResume(id string) bool {
	now := time.Now()
	if now.Sub(m.resuming[id]) < resumeWait {
		return false
	}
	m.resuming[id] = now
	return true
}

func (m Model) restore(sessions []claude.Session) tea.Cmd {
	sessions = slices.DeleteFunc(slices.Clone(sessions), func(s claude.Session) bool { return !m.claimResume(s.ID) })
	if len(sessions) == 0 {
		return func() tea.Msg { return flashMsg{text: "Everything is already open or on its way"} }
	}
	return m.do(func() error {
		for _, s := range sessions {
			if err := m.resume(s, false); err != nil {
				return err
			}
		}
		return nil
	}, fmt.Sprintf("Reopened %d sessions", len(sessions)))
}

func (m Model) detach() tea.Cmd {
	return m.do(m.host.Detach, "")
}

func (m Model) do(action func() error, success string) tea.Cmd {
	return func() tea.Msg {
		if err := action(); err != nil {
			return flashMsg{err: err}
		}
		return flashMsg{text: success}
	}
}

func (m Model) syncLabels() tea.Cmd {
	now := time.Now()
	windows := m.tabs
	byTTY := make(map[string]claude.Session)
	for _, s := range m.sessions {
		if s.Running() {
			byTTY[s.TTY] = s
		}
	}
	strip, room := 0, m.stripWidth()
	for _, w := range windows {
		if w.Dashboard || w.PoppedOut() {
			continue
		}
		if s, ok := byTTY[w.TTY]; ok && m.store.Pinned[s.ID] {
			room -= len([]rune(pinnedName(s))) + 8
		} else {
			strip++
		}
	}
	fit, compact := tabFit(room, strip)

	changed := map[string]tmux.Label{}
	position := 0
	for _, w := range windows {
		if !w.Dashboard && !w.PoppedOut() {
			position++
		}
		var label tmux.Label
		if w.Dashboard {
			label = tmux.Label{Name: "deckk", Status: "home"}
		} else if s, ok := byTTY[w.TTY]; ok {
			pinned := m.store.Pinned[s.ID]
			name := fitName(displayTitle(s), fit)
			switch {
			case pinned:
				name = pinnedName(s)
			case compact && !w.PoppedOut():
				name = strconv.Itoa(position)
			}
			label = tmux.Label{Name: name, Status: tabStatus(s), Reel: "◐", Info: tabInfo(s, now, m.bandInfo(s, pinned))}
			if m.unread(s) && !w.Active {
				label.Status = "fresh"
			}
			if label.Status == "busy" {
				label.Reel = reels[m.frame/reelEvery%len(reels)]
			}
		} else {
			continue
		}
		if m.labels[w.ID] != label {
			changed[w.ID] = label
		}
	}
	if len(changed) == 0 {
		return nil
	}
	return func() tea.Msg {
		if err := m.host.SetLabels(changed); err != nil {
			return flashMsg{err: err}
		}
		return labelsSetMsg(changed)
	}
}

// The strip spans the whole window, which is wider than the dashboard once
// the deckk session sits beside it.
func (m Model) stripWidth() int {
	for _, w := range m.tabs {
		if w.Dashboard && w.Width > 0 {
			return w.Width
		}
	}
	return m.width
}

func (m Model) bandInfo(s claude.Session, pinned bool) bandInfo {
	b := bandInfo{width: m.stripWidth(), pinned: pinned}
	if sg, ok := m.suggestion(s); ok {
		b.suggestion = sg.Prompt
	}
	if c, ok := m.clash(s); ok {
		b.warning, b.clash = "also edited by "+displayTitle(c.Other(s.ID))+": "+fileList(c.Files, 2), true
	} else if all := deck.CollisionsOf(m.collisions, s.ID); len(all) > 0 {
		b.warning = "shares its checkout with " + displayTitle(all[0].Other(s.ID))
	}
	return b
}

func tabStatus(s claude.Session) string {
	switch deck.StateOf(s) {
	case deck.Working:
		return "busy"
	case deck.YourTurn:
		return "idle"
	default:
		return "other"
	}
}

func (m Model) restoreWorkspace() tea.Cmd {
	return m.restore(deck.ToRestore(m.sessions, m.marks()))
}

const maxTabName = 24

func tabName(title string) string { return fitName(title, maxTabName) }

// tmux reads # as a format or style, and a trailing ; ends a command.
func fitName(title string, max int) string {
	r := []rune(strings.TrimRight(title, "; "))
	if len(r) > max {
		r = append(r[:max-1], '…')
	}
	return strings.ReplaceAll(string(r), "#", "##")
}

// The numbers mirror the tab formats in tmux/config.go: each tab costs its name
// plus 7 cells, the dashboard tab 12, and the buttons on the right 16.
func tabFit(width, tabs int) (int, bool) {
	if tabs == 0 {
		return maxTabName, false
	}
	per := (width - 1 - 12 - 16 - 1) / tabs
	fit := min(per-7, maxTabName)
	return max(fit, 1), fit < 6
}

// One stray key or click would otherwise rearrange all the tabs.
func (m *Model) confirmed(key, last string, sessions int, what string, run func() tea.Cmd) tea.Cmd {
	if sessions == 0 || last == key {
		m.confirm = ""
		return run()
	}
	m.confirm = key
	m.setFlash(fmt.Sprintf("Press %s again to %s", key, strings.ReplaceAll(what, "%s", sessionsWord(sessions))), nil)
	return nil
}

func sessionsWord(n int) string {
	if n == 1 {
		return "the 1 session"
	}
	return fmt.Sprintf("all %d sessions", n)
}

func (m Model) popAllCount() int {
	n := 0
	for _, s := range m.sessions {
		if _, ok := m.window(s); ok {
			n++
		}
	}
	return n
}

func (m Model) adoptAllCount() int {
	n := 0
	for _, s := range m.sessions {
		if _, popped := m.poppedOut(s); popped || m.elsewhere(s) {
			n++
		}
	}
	return n
}

func (m *Model) confirmPopAll(last string) tea.Cmd {
	return m.confirmed("O", last, m.popAllCount(), "give %s their own tab", m.popAll)
}

func (m *Model) confirmAdoptAll(last string) tea.Cmd {
	return m.confirmed("M", last, m.adoptAllCount(), "bring %s into deckk", m.adoptAll)
}
