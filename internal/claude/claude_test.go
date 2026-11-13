package claude

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const darkMode = "testdata/projects/-Users-me-app/11111111-aaaa-bbbb-cccc-000000000001.jsonl"

func TestReadTranscriptPrefersNewestMarkers(t *testing.T) {
	tr, err := ReadTranscript(darkMode)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "custom title", tr.CustomTitle, "dark mode")
	assertEqual(t, "ai title", tr.AITitle, "Dark mode toggle and ship")
	assertEqual(t, "last prompt", tr.LastPrompt, "now ship it")
	assertEqual(t, "last reply", tr.LastReply, "Sure.")
	assertEqual(t, "pr", tr.PR, "#42")
	assertEqual(t, "branch", tr.Branch, "feat/dark")
	assertEqual(t, "cwd ignores sidechains", tr.Cwd, "/Users/me/app")
	assertEqual(t, "dir", tr.Dir, "/Users/me/app")
	assertEqual(t, "first prompt", tr.FirstPrompt, "")
	assertEqual(t, "title", tr.Session().Title, "dark mode")
	if tr.Session().Background {
		t.Error("an interactive session is not background")
	}
}

func TestReadTranscriptFallsBackToFirstPrompt(t *testing.T) {
	tr, err := ReadTranscript("testdata/projects/-Users-me-app/22222222-aaaa-bbbb-cccc-000000000002.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "first prompt", tr.FirstPrompt, "fix the flaky login test please")
	assertEqual(t, "title", tr.Session().Title, "fix the flaky login test please")
	if !tr.Session().Background {
		t.Error("a session started by claude -p belongs in the background")
	}
}

func TestReadTranscriptOnlyReadsTheTail(t *testing.T) {
	data, err := os.ReadFile(darkMode)
	if err != nil {
		t.Fatal(err)
	}
	filler := `{"type":"assistant","message":{"content":"` + strings.Repeat("x", 300<<10) + `"}}` + "\n"
	path := filepath.Join(t.TempDir(), "big.jsonl")
	if err := os.WriteFile(path, append([]byte(filler), data...), 0o600); err != nil {
		t.Fatal(err)
	}

	tr, err := ReadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "last prompt", tr.LastPrompt, "now ship it")
	assertEqual(t, "cwd", tr.Cwd, "/Users/me/app")
}

func TestScanSkipsEmptyAndStaleTranscripts(t *testing.T) {
	transcripts, err := ScanTranscripts("testdata/projects", time.Time{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(transcripts) != 2 {
		t.Fatalf("got %d transcripts, want 2", len(transcripts))
	}

	transcripts, err = ScanTranscripts("testdata/projects", time.Now().Add(time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(transcripts) != 0 {
		t.Fatalf("got %d transcripts, want none newer than the future", len(transcripts))
	}

	keep := map[string]bool{"11111111-aaaa-bbbb-cccc-000000000001": true}
	transcripts, err = ScanTranscripts("testdata/projects", time.Now().Add(time.Hour), keep)
	if err != nil {
		t.Fatal(err)
	}
	if len(transcripts) != 1 {
		t.Fatalf("got %d transcripts, want the kept one whatever its age", len(transcripts))
	}
}

func TestParsePS(t *testing.T) {
	procs := parsePS(`  101 ttys001  Mon Nov  9 16:42:09 2026     claude
  102 ??       Sun Oct 11 14:38:44 2026     /usr/bin/claude
  103 ttys004  Sun Oct 11 14:38:44 2026     node
`)
	if procs[101].started.IsZero() {
		t.Error("start time should parse")
	}
	assertEqual(t, "tty", procs[101].tty, "/dev/ttys001")
	assertEqual(t, "detached tty", procs[102].tty, "")
	if !procs[102].claude || procs[103].claude {
		t.Errorf("claude detection wrong: %+v", procs)
	}
}

func TestSameStartComparesUTCWithLocal(t *testing.T) {
	local := time.Date(2026, 11, 9, 9, 42, 9, 0, time.FixedZone("PST", -8*3600))
	if !sameStart("Mon Nov  9 17:42:09 2026", local) {
		t.Error("the same instant in UTC and local time should match")
	}
	if sameStart("Mon Nov  9 17:50:00 2026", local) {
		t.Error("a different start means the PID was reused")
	}
}

func TestLiveNameOnlyWinsWhenUserSet(t *testing.T) {
	s := Session{Title: "Dark mode toggle"}
	Live{PID: 1, Name: "app-1f", NameSource: "derived", Status: "busy"}.applyTo(&s)
	assertEqual(t, "derived name", s.Title, "Dark mode toggle")
	assertEqual(t, "status", s.Status, "busy")

	Live{PID: 1, Name: "my rename", NameSource: "user"}.applyTo(&s)
	assertEqual(t, "user name", s.Title, "my rename")

	fresh := Session{}
	Live{PID: 1, Name: "dev-a9", NameSource: "derived"}.applyTo(&fresh)
	assertEqual(t, "placeholder name", fresh.Title, "")
}

func TestReadLiveDropsDeadProcesses(t *testing.T) {
	live, err := ReadLive("testdata/sessions")
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 0 {
		t.Fatalf("pid 999999 should not be alive, got %+v", live)
	}
}

func assertEqual(t *testing.T, what, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %q, want %q", what, got, want)
	}
}

func TestProjectDirs(t *testing.T) {
	got := ProjectDirs("/h", "/Users/me/dev/slow_down")
	want := []string{"/h/projects/-Users-me-dev-slow_down", "/h/projects/-Users-me-dev-slow-down"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v", got)
	}
	if got := ProjectDirs("/h", "/Users/me/.config/deckk/hq"); len(got) != 1 || got[0] != "/h/projects/-Users-me--config-deckk-hq" {
		t.Fatalf("got %v", got)
	}
}

func TestOnlyInteractiveSessionsAreYours(t *testing.T) {
	cases := []struct {
		live       Live
		background bool
	}{
		{Live{Entrypoint: "cli", Kind: "interactive"}, false},
		{Live{Entrypoint: "cli"}, false},
		{Live{Entrypoint: "sdk-cli", Kind: "interactive"}, true},
		{Live{Entrypoint: "cli", Kind: "background"}, true},
	}
	for _, c := range cases {
		var s Session
		c.live.applyTo(&s)
		if s.Background != c.background {
			t.Errorf("%+v: background %v", c.live, s.Background)
		}
	}
}
