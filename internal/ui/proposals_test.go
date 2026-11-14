package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/config"
	"github.com/theowecker/deckk/internal/deck"
	"github.com/theowecker/deckk/internal/hq"
)

func TestAProposedSessionStartsWithItsPromptTypedIn(t *testing.T) {
	m, host := newTestModel(t)
	m.view = homeView
	folder := t.TempDir()
	if err := hq.Propose(hq.Proposal{ID: "p1", Folder: folder, Prompt: "fix the upload crash", Why: "it blocks the release", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	m = refresh(t, m, host, nil)
	if m.groups[0].Section != deck.NextUp || !isProposal(m.groups[0].Sessions[0]) {
		t.Fatalf("groups %v", m.groups)
	}
	if row := ansi.Strip(m.suggestionRow(m.groups[0].Sessions[0], false, 30)); !strings.Contains(row, "+ New session in") || !strings.Contains(row, "fix the upload crash") {
		t.Fatalf("row %q", row)
	}

	m.cursor = 0
	m, _ = press(m, "x")
	if len(host.closed) != 0 || !strings.Contains(m.flash, "doesn't exist yet") {
		t.Fatal("session keys shouldn't act on a proposal")
	}
	tm, cmd := m.Update(keyMsg("enter"))
	m = tm.(Model)
	run(cmd)
	if len(host.opened) != 1 || !strings.HasPrefix(host.opened[0], folder+" ") {
		t.Fatalf("opened %v", host.opened)
	}

	started := claude.Session{ID: "nnnnnnnn", Title: "fresh", Cwd: folder, Dir: folder, PID: 7, TTY: "/dev/ttys107", Status: "idle", StartedAt: time.Now(), UpdatedAt: time.Now()}
	host.windows = append(host.windows, tmuxWindow("@7", "/dev/ttys107"))
	m = load(t, m, host, append(slices.Clone(m.sessions), started))
	if !slices.ContainsFunc(host.delivered, func(d typing) bool { return d.target == "/dev/ttys107" && d.text == "fix the upload crash" && !d.send }) {
		t.Fatalf("delivered %v", host.delivered)
	}
	if len(m.groups) > 0 && m.groups[0].Section == deck.NextUp {
		t.Fatal("a started proposal should leave Next up")
	}
	events, _ := hq.Unread()
	if !slices.ContainsFunc(events, func(e hq.Event) bool { return e.Kind == "was started from your proposal" }) {
		t.Fatalf("events %v", events)
	}
}

func TestAStoppedSessionCanBeOfferedForResuming(t *testing.T) {
	m, host := newTestModel(t)
	m.view = homeView
	suggestFor(t, "aaaaaaaa", hq.Suggestion{Prompt: "finish the migration", At: time.Now().Add(time.Second)})
	m = refresh(t, m, host, nil)
	if m.groups[0].Section != deck.NextUp || m.groups[0].Sessions[0].ID != "aaaaaaaa" {
		t.Fatalf("groups %v", m.groups)
	}
	m.cursor = 0
	tm, cmd := m.Update(keyMsg("enter"))
	m = tm.(Model)
	run(cmd)
	if len(host.opened) != 1 || !strings.Contains(host.opened[0], "--resume aaaaaaaa") {
		t.Fatalf("opened %v", host.opened)
	}
	host.windows = append(host.windows, tmuxWindow("@8", "/dev/ttys108"))
	m = refresh(t, m, host, func(s []claude.Session) {
		for i := range s {
			if s[i].ID == "aaaaaaaa" {
				s[i].PID, s[i].TTY, s[i].Status, s[i].StatusAt = 8, "/dev/ttys108", "idle", time.Now()
			}
		}
	})
	refresh(t, m, host, nil)
	if !slices.ContainsFunc(host.delivered, func(d typing) bool { return d.target == "/dev/ttys108" && d.text == "finish the migration" }) {
		t.Fatalf("delivered %v", host.delivered)
	}
}

func TestProposalsWaitWhileTheDeckkSessionIsOff(t *testing.T) {
	m, host := newTestModel(t)
	m.cfg.HQ = config.Off
	_ = hq.Propose(hq.Proposal{ID: "p3", Folder: t.TempDir(), Prompt: "go", At: time.Now()})
	m = refresh(t, m, host, nil)
	if len(m.groups) > 0 && m.groups[0].Section == deck.NextUp {
		t.Fatal("proposals need the deckk session on")
	}
}

func TestTwoProposalsForOneFolderGetASessionEach(t *testing.T) {
	m, host := newTestModel(t)
	folder := t.TempDir()
	now := time.Now()
	m.deckk.awaiting = []awaitingPrompt{
		{key: "proposal:a", dir: folder, after: now, prompt: "first"},
		{key: "proposal:b", dir: folder, after: now, prompt: "second"},
	}
	one := claude.Session{ID: "11111111", Cwd: folder, PID: 11, TTY: "/dev/ttys111", Status: "idle", StartedAt: now}
	host.windows = append(host.windows, tmuxWindow("@11", "/dev/ttys111"))
	m = load(t, m, host, append(slices.Clone(m.sessions), one))
	if len(host.delivered) != 1 || host.delivered[0].text != "first" {
		t.Fatalf("delivered %v", host.delivered)
	}
	two := claude.Session{ID: "22222222", Cwd: folder, PID: 12, TTY: "/dev/ttys112", Status: "idle", StartedAt: now}
	host.windows = append(host.windows, tmuxWindow("@12", "/dev/ttys112"))
	load(t, m, host, append(slices.Clone(m.sessions), two))
	if len(host.delivered) != 2 || host.delivered[1].target != "/dev/ttys112" || host.delivered[1].text != "second" {
		t.Fatalf("delivered %v", host.delivered)
	}
}

func TestAProposedFolderMatchesThroughASymlink(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "app")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	resolved, _ := filepath.EvalSymlinks(real)
	if !sameDir(resolved, link) || sameDir(resolved, t.TempDir()) {
		t.Fatal("a folder and a link to it are the same place")
	}
}
