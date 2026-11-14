package hq

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theowecker/deckk/internal/claude"
)

func TestParseReview(t *testing.T) {
	if r := parseReview("OK\n"); !r.OK || r.Text != "" {
		t.Fatalf("%+v", r)
	}
	r := parseReview("**CONCERNS**\n- debug print left in api.go\n- no test for the retry")
	if r.OK || r.Text != "- debug print left in api.go\n- no test for the retry" {
		t.Fatalf("%+v", r)
	}
	if r := parseReview("I looked and it seems fine"); r.OK || r.Text == "" {
		t.Fatalf("an answer without a verdict counts as a concern to read: %+v", r)
	}
}

func TestSoundsDone(t *testing.T) {
	for reply, want := range map[string]bool{
		"Tests pass and the build is clean. Ready to commit when you are.": true,
		"Committed as a1b2c3 and pushed to the branch.":                    true,
		"Opened a PR: #212": true,
		"Should the retry live in the client or the server?":     false,
		"I read the handler; the bug is in how it parses dates.": false,
	} {
		if SoundsDone(reply) != want {
			t.Errorf("%q: want %v", reply, want)
		}
	}
}

func TestReviewSessionReusesAReviewUnlessAskedForAFreshOne(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	bin := t.TempDir()
	calls := filepath.Join(bin, "calls")
	script := "#!/bin/sh\necho call >> '" + calls + "'\nprintf 'CONCERNS\\n- debug print left in\\n'\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))

	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "first"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%s", out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "wip.go"), []byte("package wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := claude.Session{ID: "rrrrrrrr", Title: "Work", Cwd: repo}
	count := func() int {
		data, _ := os.ReadFile(calls)
		return strings.Count(string(data), "call")
	}

	r, err := ReviewSession(context.Background(), s, "", false)
	if err != nil || r.OK || r.Text != "- debug print left in" || count() != 1 {
		t.Fatalf("first review %+v, err %v, calls %d", r, err, count())
	}
	if again, _ := ReviewSession(context.Background(), s, "", false); !again.At.Equal(r.At) || count() != 1 {
		t.Fatalf("unchanged work should keep its review, calls %d", count())
	}
	if _, err := ReviewSession(context.Background(), s, "", true); err != nil || count() != 2 {
		t.Fatalf("a fresh review should run again, calls %d, err %v", count(), err)
	}
}
