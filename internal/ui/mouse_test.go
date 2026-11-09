package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func click(m Model, x, y int) (Model, tea.Cmd) {
	tm, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	return tm.(Model), cmd
}

func rowY(m Model, index int) int {
	_, owners := m.body()
	for line, owner := range owners {
		if owner == index {
			return line + headerLines - m.offset
		}
	}
	return -1
}

func TestClickSelectsAndDoubleClickOpens(t *testing.T) {
	m, host := newTestModel(t)
	target := indexOf(m, "bbbbbbbb")

	m, cmd := click(m, 10, rowY(m, target))
	if m.cursor != target || cmd != nil {
		t.Fatalf("single click: cursor %d, cmd %v", m.cursor, cmd)
	}

	_, cmd = click(m, 10, rowY(m, target))
	run(cmd)
	if len(host.opened) != 1 {
		t.Fatalf("double click should resume the session, opened %v", host.opened)
	}
}

func TestClickingHeaderOrBlankDoesNothing(t *testing.T) {
	m, _ := newTestModel(t)
	m, _ = click(m, 10, 0)
	m, _ = click(m, 10, headerLines)
	if m.cursor != 0 {
		t.Fatalf("cursor moved to %d", m.cursor)
	}
}

func TestClickingFooterHintRunsIt(t *testing.T) {
	m, _ := newTestModel(t)
	x := -1
	for i := range m.width {
		if key, ok := m.hintAt(i); ok && key == "p" {
			x = i
			break
		}
	}
	if x < 0 {
		t.Fatal("pin hint not found")
	}
	selected, _ := m.selected()
	m, _ = click(m, x, m.footerY())
	if !m.store.Pinned[selected.ID] {
		t.Fatal("clicking pin should pin the selected session")
	}
}

func TestWheelMovesSelection(t *testing.T) {
	m, _ := newTestModel(t)
	tm, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if tm.(Model).cursor != 1 {
		t.Fatalf("cursor %d", tm.(Model).cursor)
	}
}

func TestHoverOnlyChangesPointerWhenNeeded(t *testing.T) {
	m, _ := newTestModel(t)
	if cmd := m.hover(10, rowY(m, 0)); cmd == nil || m.pointer != "pointer" {
		t.Fatalf("hovering a row should show the hand, got %q", m.pointer)
	}
	if cmd := m.hover(12, rowY(m, 0)); cmd != nil {
		t.Fatal("moving within a row should not resend the pointer")
	}
	if m.hover(10, 0); m.pointer != "default" {
		t.Fatalf("the header should show the arrow, got %q", m.pointer)
	}
}

func TestDoubleClickBelowTheSelectionOpensTheClickedRow(t *testing.T) {
	m, host := newTestModel(t)
	m.sessions[0].LastPrompt = "a prompt that adds preview lines to the selected row"
	m.sessions[0].LastReply = "and a reply"
	m.rearrange()
	target := indexOf(m, "cccccccc")
	y := rowY(m, target)

	m, _ = click(m, 10, y)
	_, cmd := click(m, 10, y)
	run(cmd)
	if len(host.opened) != 1 || !strings.HasSuffix(host.opened[0], "cccccccc") {
		t.Fatalf("opened %v", host.opened)
	}
}

func TestScrollingKeepsThePreviewVisible(t *testing.T) {
	m, _ := newTestModel(t)
	for i := range m.sessions {
		m.sessions[i].LastPrompt = "prompt"
		m.sessions[i].LastReply = "reply"
	}
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	m = tm.(Model)
	m.rearrange()
	m.move(len(m.flat))

	_, owners := m.body()
	last := 0
	for i, o := range owners {
		if o == m.cursor {
			last = i
		}
	}
	if last >= m.offset+m.bodyHeight() {
		t.Fatalf("reply line %d is below the visible area ending at %d", last, m.offset+m.bodyHeight())
	}
}
