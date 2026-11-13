package hq

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
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
