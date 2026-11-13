package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/hq"
)

func clashing(t *testing.T) (Model, *fakeHost) {
	t.Helper()
	m, host := withDeckk(t)
	host.edits = map[string][]string{
		"dddddddd": {"/shop/cart.go", "/shop/tax.go"},
		"ffffffff": {"/shop/cart.go"},
	}
	return refresh(t, m, host, nil), host
}

func TestAClashIsShownAboveTheListOnTheRowAndOnTheTab(t *testing.T) {
	m, host := clashing(t)
	m.startedAt = time.Time{}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "⚠ Inside deckk and Popped out both edited cart.go") && !strings.Contains(view, "⚠ Popped out and Inside deckk both edited cart.go") {
		t.Fatalf("no banner in:\n%s", view)
	}
	row := ansi.Strip(m.row(*inside(m.sessions), false, m.sessions[0].UpdatedAt, 20, 10))
	if !strings.Contains(row, "⚠") {
		t.Fatalf("row %q", row)
	}
	run(m.syncLabels())
	if info := host.labels["@1"].Info; !strings.Contains(info, "also edited by Popped out: cart.go") {
		t.Fatalf("band %q", info)
	}
}

func TestTheDeckkSessionHearsOfAClashOnce(t *testing.T) {
	m, host := clashing(t)
	refresh(t, m, host, nil)
	events, err := hq.Unread()
	if err != nil {
		t.Fatal(err)
	}
	var clashes int
	for _, e := range events {
		if strings.Contains(e.Kind, "edited the same files as") {
			clashes++
		}
	}
	if clashes != 1 {
		t.Fatalf("events %v", events)
	}
}

func TestSharingACheckoutIsOnlyANote(t *testing.T) {
	m, host := withDeckk(t)
	if err := os.Mkdir(filepath.Join(inside(m.sessions).Cwd, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	m = refresh(t, m, host, nil)
	lines := m.collisionLines(claude.Session{ID: "dddddddd"})
	if len(m.clashBanner()) != 0 || len(lines) == 0 || !strings.HasPrefix(lines[0], "⇄ shares") {
		t.Fatalf("banner %v, lines %v", m.clashBanner(), lines)
	}
}

func TestAClashIsOnlyNewsWhenItSpreads(t *testing.T) {
	m, host := clashing(t)
	before, _ := hq.Unread()
	host.edits["dddddddd"] = []string{"/shop/tax.go", "/shop/cart.go"}
	m = refresh(t, m, host, nil)
	if again, _ := hq.Unread(); len(again) != 0 {
		t.Fatalf("editing the same files again is not news: %v (first %v)", again, before)
	}
	host.edits["dddddddd"] = append(host.edits["dddddddd"], "/shop/order.go")
	host.edits["ffffffff"] = append(host.edits["ffffffff"], "/shop/order.go")
	refresh(t, m, host, nil)
	if spread, _ := hq.Unread(); len(spread) != 1 || !strings.Contains(spread[0].Detail, "order.go") {
		t.Fatalf("a newly shared file is news: %v", spread)
	}
}
