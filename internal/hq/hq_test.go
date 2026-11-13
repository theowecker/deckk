package hq

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theowecker/deckk/internal/mcp"
)

func TestAQuestionWaitsForItsAnswer(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	q := Question{ID: newID(), Session: "s1", Title: "Checkout", Text: "what next?", Wait: true, At: time.Now()}
	if err := Post(q); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		if err := Answer(q.ID, "open the PR"); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	answer, ok := Await(ctx, q.ID)
	if !ok || answer != "open the PR" {
		t.Fatalf("got %q %v", answer, ok)
	}
	if left, _ := Questions(); len(left) != 0 {
		t.Fatalf("answered questions should be gone: %v", left)
	}
	if err := Answer(q.ID, "late"); err == nil {
		t.Fatal("answering a question nobody waits on should fail")
	}
}

func TestNotesAreDeliveredOnce(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	note := Question{ID: newID(), Title: "API", Text: "merged the cursor change", At: time.Now()}
	waiting := Question{ID: newID(), Title: "Client", Text: "should I rebase?", Wait: true, At: time.Now()}
	for _, q := range []Question{note, waiting} {
		if err := Post(q); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := Questions()
	Delivered(all)
	left, _ := Questions()
	if len(left) != 1 || left[0].ID != waiting.ID {
		t.Fatalf("left %v", left)
	}
}

func TestTheInboxOnlyShowsWhatsNew(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	first := time.Now()
	if err := AppendEvents([]Event{{At: first, Title: "Checkout", Kind: Finished}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := Unread(); len(got) != 1 || got[0].Line() != "Checkout finished" {
		t.Fatalf("got %v", got)
	}
	if got, _ := Unread(); len(got) != 0 {
		t.Fatalf("read twice: %v", got)
	}
	_ = AppendEvents([]Event{{At: first.Add(time.Second), Title: "API", Kind: OpenedPR, Detail: "#12"}})
	if got, _ := Unread(); len(got) != 1 || got[0].Line() != "API opened a PR: #12" {
		t.Fatalf("got %v", got)
	}
}

func TestTheInboxStaysSmall(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	start := time.Now()
	for i := range inboxKeep*2 + 5 {
		_ = AppendEvents([]Event{{At: start.Add(time.Duration(i) * time.Millisecond), Title: "s", Kind: Finished}})
	}
	all, _ := readInbox()
	if len(all) > inboxKeep*2 {
		t.Fatalf("inbox kept %d events", len(all))
	}
}

func TestSetupKeepsTheUsersNotes(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Setup("/bin/deckk"); err != nil {
		t.Fatal(err)
	}
	if err := WriteGoals("ship it"); err != nil {
		t.Fatal(err)
	}
	mine := claudeMD + "\nI like short answers.\n"
	if err := writeAtomic(path("CLAUDE.md"), []byte(mine)); err != nil {
		t.Fatal(err)
	}
	if err := Setup("/bin/deckk"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, "CLAUDE.md"); got != mine {
		t.Fatalf("CLAUDE.md was rewritten: %q", got)
	}
	if !strings.Contains(read(t, "mcp.json"), `"/bin/deckk"`) || !strings.Contains(read(t, "role.md"), "# You are deckk") {
		t.Fatal("setup should write the tool config and role")
	}
}

func read(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(path(name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestProposingASession(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := propose(mcp.Args{"folder": "/no/such/folder", "prompt": "go"}); err == nil {
		t.Fatal("a missing folder should be refused")
	}
	folder := t.TempDir()
	if _, err := propose(mcp.Args{"folder": folder, "prompt": "  fix\nthe crash  "}); err != nil {
		t.Fatal(err)
	}
	if err := Propose(Proposal{ID: "old", Folder: folder, Prompt: "stale", At: time.Now().Add(-4 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	list, err := LoadProposals()
	if err != nil || len(list) != 1 || list[0].Prompt != "fix the crash" {
		t.Fatalf("got %v %v", list, err)
	}
	if err := Withdraw(list[0].ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := LoadProposals(); len(list) != 0 {
		t.Fatalf("withdrawn proposal still listed: %v", list)
	}
}

func TestNotesAreKeptAndForgotten(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Remember("s1", "cart fix, waiting on review"); err != nil {
		t.Fatal(err)
	}
	notes, err := LoadNotes()
	if err != nil || notes["s1"].Text != "cart fix, waiting on review" || notes["s1"].At.IsZero() {
		t.Fatalf("notes %v %v", notes, err)
	}
	_ = Remember("s1", "")
	if notes, _ := LoadNotes(); len(notes) != 0 {
		t.Fatalf("an empty note should forget: %v", notes)
	}
}

func TestAnswersOnlyTakeRealQuestionIDs(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Answer("../../etc/x", "hi"); err == nil {
		t.Fatal("a path should not be accepted as a question ID")
	}
}

func TestIsSeesThroughSymlinks(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "config")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", link)
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, _ := filepath.EvalSymlinks(Dir())
	if !Is(Dir()) || !Is(resolved) || Is(filepath.Dir(resolved)) {
		t.Fatalf("Is %q", resolved)
	}
}

func TestSetupPicksTheBriefStyleAndKeepsOtherSettings(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	settings := filepath.Join(Dir(), ".claude", "settings.json")
	if err := writeAtomic(settings, []byte(`{"model":"sonnet"}`)); err != nil {
		t.Fatal(err)
	}
	if err := Setup("/bin/deckk"); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(".claude", "settings.json"))
	if !strings.Contains(got, `"outputStyle": "deckk brief"`) || !strings.Contains(got, `"model": "sonnet"`) {
		t.Fatalf("settings %s", got)
	}
	if style := read(t, filepath.Join(".claude", "output-styles", "deckk-brief.md")); !strings.HasPrefix(style, "---\nname: deckk brief\n") {
		t.Fatalf("style %q", style[:40])
	}
}
