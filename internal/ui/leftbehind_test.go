package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theowecker/deckk/internal/claude"
)

func TestLeftBehindFindsQuietCheckoutsWithWork(t *testing.T) {
	repo := func(dirty bool) string {
		dir, _ := filepath.EvalSymlinks(t.TempDir())
		for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "first"}} {
			cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s", out)
			}
		}
		if dirty {
			if err := os.WriteFile(filepath.Join(dir, "wip.txt"), []byte("half done\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	now := time.Now()
	quiet, clean, busy, recent := repo(true), repo(false), repo(true), repo(true)
	sessions := []claude.Session{
		{ID: "q", Title: "Quiet", Cwd: quiet, UpdatedAt: now.Add(-4 * 24 * time.Hour)},
		{ID: "c", Title: "Clean", Cwd: clean, UpdatedAt: now.Add(-4 * 24 * time.Hour)},
		{ID: "b1", Title: "Busy old", Cwd: busy, UpdatedAt: now.Add(-5 * 24 * time.Hour)},
		{ID: "b2", Title: "Busy now", Cwd: busy, UpdatedAt: now, PID: os.Getpid(), TTY: "/dev/ttys999"},
		{ID: "r", Title: "Recent", Cwd: recent, UpdatedAt: now.Add(-time.Hour)},
	}
	left := leftBehindIn(sessions, now)
	if len(left) != 1 || left[0].session.ID != "q" {
		t.Fatalf("left %+v", left)
	}
	if line := left[0].line(now); !strings.Contains(line, "1 uncommitted change") || !strings.Contains(line, "untouched 4 days") {
		t.Fatalf("line %q", line)
	}
}
