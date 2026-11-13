package hq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/git"
)

type Review struct {
	At time.Time `json:"at"`
	// Key identifies the diff reviewed, so unchanged work isn't reviewed again.
	Key  string `json:"key"`
	OK   bool   `json:"ok"`
	Text string `json:"text"`
}

func LoadReviews() (map[string]Review, error) {
	all := map[string]Review{}
	data, err := os.ReadFile(path("reviews.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return all, nil
	} else if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, err
	}
	return all, nil
}

func saveReview(id string, r Review) error {
	return locked(func() error {
		all, err := LoadReviews()
		if err != nil {
			return err
		}
		all[id] = r
		data, err := json.MarshalIndent(all, "", "  ")
		if err != nil {
			return err
		}
		return writeAtomic(path("reviews.json"), data)
	})
}

// ErrNothingToReview means the session's checkout has no changes.
var ErrNothingToReview = errors.New("no changes to review")

const reviewDiffLimit = 60_000

// ReviewSession checks what a session says it did against what its checkout
// changed, with a quick model and no tools. Unchanged work keeps its review
// unless fresh asks for a new one.
func ReviewSession(ctx context.Context, s claude.Session, note string, fresh bool) (Review, error) {
	diff, key := git.Diff(s.Cwd)
	if diff == "" {
		return Review{}, ErrNothingToReview
	}
	diff = git.Cut(diff, reviewDiffLimit)
	if all, err := LoadReviews(); err == nil && all[s.ID].Key == key && !fresh {
		return all[s.ID], nil
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "-p", "--model", "haiku", "--tools", "", "--no-session-persistence")
	cmd.Dir = os.TempDir()
	cmd.Stdin = strings.NewReader(reviewPrompt(s, note, diff))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return Review{}, fmt.Errorf("review failed: %w %s", err, strings.TrimSpace(stderr.String()))
	}
	r := parseReview(string(out))
	r.At, r.Key = time.Now(), key
	return r, saveReview(s.ID, r)
}

func reviewPrompt(s claude.Session, note, diff string) string {
	var b strings.Builder
	b.WriteString(reviewInstructions)
	fmt.Fprintf(&b, "\n\nSession: %s\nFolder: %s\nBranch: %s\n", s.Title, s.Cwd, s.Branch)
	if s.PR != "" {
		fmt.Fprintf(&b, "Pull request: %s\n", s.PR)
	}
	if note != "" {
		fmt.Fprintf(&b, "\nWhat the coordinator noted about it:\n%s\n", note)
	}
	fmt.Fprintf(&b, "\nThe user's last prompt:\n%s\n\nClaude's last reply:\n%s\n\nThe changes:\n%s\n",
		clipText(s.LastPrompt, 2000), clipText(s.LastReply, 4000), diff)
	return b.String()
}

const reviewInstructions = `You give a coding session's work a quick sanity check. You see what the session says it did and the changes in its checkout.

Answer with a first line that is exactly OK or CONCERNS. If CONCERNS, follow it with at most five short bullets, each one line, naming:
- a mismatch between what was said and what changed
- a likely bug
- leftover debug code, TODOs or commented-out code
- a risky change with no test

No praise, no summary of the changes, no headings. If nothing stands out, answer OK alone.`

func parseReview(out string) Review {
	out = strings.TrimSpace(out)
	first, rest, _ := strings.Cut(out, "\n")
	verdict := strings.ToUpper(strings.Trim(strings.TrimSpace(first), "*#: "))
	switch verdict {
	case "OK":
		return Review{OK: true, Text: strings.TrimSpace(rest)}
	case "CONCERNS":
		return Review{Text: strings.TrimSpace(rest)}
	}
	return Review{Text: out}
}

func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

var doneSignals = []string{
	"ready to commit", "ready for review", "ready to merge", "ready to push", "ready to ship",
	"committed", "pushed", "opened a pr", "opened pr", "pull request",
	"tests pass", "tests are passing", "all green", "lint is clean",
	"all done", "is done", "is complete", "is finished", "work is finished", "implemented",
}

// SoundsDone guesses from Claude's reply whether it considers the work
// finished, which is when a review is worth its cost.
func SoundsDone(reply string) bool {
	reply = strings.ToLower(reply)
	for _, signal := range doneSignals {
		if strings.Contains(reply, signal) {
			return true
		}
	}
	return false
}
