package ui

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/config"
	"github.com/theowecker/deckk/internal/store"
	"github.com/theowecker/deckk/internal/tmux"
)

type fakeHost struct {
	windows  []tmux.Window
	opened   []string
	selected []string
	closed   []string
	released []string
	popped   []string
	brought  []string
	moved    []string

	hqOpened, hqClosed int
	hqPane             tmux.Pane
	delivered          []typing
	// prompts holds what each pane's prompt shows, by pane ID or tty.
	prompts map[string]string
	labels  map[string]tmux.Label
	tabKeys []string
}

func (h *fakeHost) PopOut(id string) error    { h.popped = append(h.popped, id); return nil }
func (h *fakeHost) BringBack(id string) error { h.brought = append(h.brought, id); return nil }

func (h *fakeHost) MoveAfter(id, after string) error {
	h.moved = append(h.moved, id+">"+after)
	return nil
}

func (h *fakeHost) Focus(string) error { return nil }

func (h *fakeHost) OpenHQ(focus bool) error { h.hqOpened++; return nil }
func (h *fakeHost) CloseHQ() error          { h.hqClosed++; return nil }

func (h *fakeHost) HQPane() (tmux.Pane, bool, error) { return h.hqPane, h.hqPane.ID != "", nil }

func (h *fakeHost) Deliver(target, text, replace string, send bool) (tmux.Delivery, error) {
	if h.prompts == nil {
		h.prompts = map[string]string{}
	}
	if typed := h.prompts[target]; typed != "" && typed != replace {
		return tmux.Busy, nil
	}
	h.delivered = append(h.delivered, typing{target, text, replace, send})
	h.prompts[target] = text
	if send {
		h.prompts[target] = ""
	}
	return tmux.Delivered, nil
}

func (h *fakeHost) SetTabKeys(preset string) error {
	h.tabKeys = append(h.tabKeys, preset)
	return nil
}

func (h *fakeHost) Release(pid int, tty string) error {
	h.released = append(h.released, tty)
	return nil
}

func (h *fakeHost) Windows() ([]tmux.Window, error) { return h.windows, nil }

func (h *fakeHost) Open(dir, name, command string, focus bool) error {
	h.opened = append(h.opened, dir+" "+command)
	return nil
}

func (h *fakeHost) Select(id string) error { h.selected = append(h.selected, id); return nil }
func (h *fakeHost) Close(id string) error  { h.closed = append(h.closed, id); return nil }
func (h *fakeHost) Detach() error          { return nil }

func (h *fakeHost) SetLabels(labels map[string]tmux.Label) error {
	h.labels = labels
	return nil
}

func newTestModel(t *testing.T) (Model, *fakeHost) {
	t.Helper()
	return newTestModelWith(t, nil)
}

// prepare stands in for state left by an earlier run of deckk.
func newTestModelWith(t *testing.T, prepare func(*store.Store)) (Model, *fakeHost) {
	t.Helper()
	t.Setenv("TMUX", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if prepare != nil {
		prepare(st)
	}
	dir := t.TempDir()
	now := time.Now()
	sessions := []claude.Session{
		{ID: "dddddddd", Title: "Inside deckk", Cwd: dir, UpdatedAt: now, PID: 1, TTY: "/dev/ttys101", Status: "busy"},
		{ID: "ffffffff", Title: "Popped out", Cwd: dir, UpdatedAt: now.Add(-2 * time.Minute), PID: 3, TTY: "/dev/ttys102", Status: "busy"},
		{ID: "eeeeeeee", Title: "Other iTerm tab", Cwd: dir, UpdatedAt: now.Add(-time.Minute), PID: 2, TTY: "/dev/ttys200", Status: "idle"},
		{ID: "aaaaaaaa", Title: "First", Cwd: dir, UpdatedAt: now},
		{ID: "bbbbbbbb", Title: "Second", Cwd: dir, UpdatedAt: now.Add(-time.Hour)},
		{ID: "cccccccc", Title: "Third", Cwd: dir, UpdatedAt: now.Add(-2 * time.Hour)},
	}
	for i := range sessions {
		sessions[i].Dir = sessions[i].Cwd
	}
	host := &fakeHost{windows: []tmux.Window{
		{ID: "@0", TTY: "/dev/ttys100", Dashboard: true, Session: "deckk"},
		{ID: "@1", TTY: "/dev/ttys101", Session: "deckk"},
		{ID: "@2", TTY: "/dev/ttys102", Session: "deckk-2", ClientTTY: "/dev/ttys300"},
	}}
	cfg, err := config.Load(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Ask = config.Off
	var tm tea.Model = New("", time.Time{}, st, cfg, host)
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	tm, cmd := tm.Update(loadedMsg{sessions: sessions, windows: host.windows})
	run(cmd)
	return tm.(Model), host
}

// Timers are skipped so tests stay instant.
func run(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		// tea.Sequence's message is unexported but is a slice of commands too.
		v := reflect.ValueOf(msg)
		if v.Kind() != reflect.Slice || v.Type().Elem() != reflect.TypeFor[tea.Cmd]() {
			return []tea.Msg{msg}
		}
		var msgs []tea.Msg
		for i := range v.Len() {
			msgs = append(msgs, run(v.Index(i).Interface().(tea.Cmd))...)
		}
		return msgs
	case <-time.After(50 * time.Millisecond):
		return nil
	}
}

func press(m Model, key string) (Model, tea.Cmd) {
	tm, cmd := m.Update(keyMsg(key))
	return tm.(Model), cmd
}

func indexOf(m Model, id string) int {
	for i, s := range m.flat {
		if s.ID == id {
			return i
		}
	}
	return -1
}

func selectID(m Model, id string) Model {
	m.move(indexOf(m, id) - m.cursor)
	return m
}

type typing struct {
	target, text, replace string
	send                  bool
}
