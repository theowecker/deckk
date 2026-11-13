package hq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"time"
)

// A Question is a session talking to the deckk session. When Wait is set the
// asking session blocks until it is answered; otherwise it is a note that
// read_inbox delivers once. Each lives in its own file, since every session
// writes its own.
type Question struct {
	ID      string    `json:"id"`
	Session string    `json:"session"`
	Title   string    `json:"title"`
	Text    string    `json:"text"`
	Wait    bool      `json:"wait"`
	At      time.Time `json:"at"`
}

func questionPath(id string) string { return filepath.Join(Dir(), "questions", id+".json") }
func answerPath(id string) string   { return filepath.Join(Dir(), "answers", id+".txt") }

func Post(q Question) error {
	if err := os.MkdirAll(filepath.Dir(questionPath(q.ID)), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(q)
	if err != nil {
		return err
	}
	return writeAtomic(questionPath(q.ID), data)
}

func Await(ctx context.Context, id string) (string, bool) {
	defer func() { _ = os.Remove(questionPath(id)) }()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		if data, err := os.ReadFile(answerPath(id)); err == nil {
			_ = os.Remove(answerPath(id))
			return string(data), true
		}
		select {
		case <-ctx.Done():
			return "", false
		case <-tick.C:
		}
	}
}

// Question IDs end up in file names, so only ones deckk could have made are
// accepted.
var validID = regexp.MustCompile(`^[a-z0-9]{1,32}$`)

func Answer(id, text string) error {
	if !validID.MatchString(id) {
		return fmt.Errorf("%q is not a question ID", id)
	}
	if _, err := os.Stat(questionPath(id)); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("question %s is no longer waiting", id)
	}
	if err := os.MkdirAll(filepath.Dir(answerPath(id)), 0o755); err != nil {
		return err
	}
	return writeAtomic(answerPath(id), []byte(text))
}

func Questions() ([]Question, error) {
	paths, err := filepath.Glob(filepath.Join(Dir(), "questions", "*.json"))
	if err != nil {
		return nil, err
	}
	var all []Question
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var q Question
		if json.Unmarshal(data, &q) != nil || q.expired(time.Now()) {
			_ = os.Remove(p)
			continue
		}
		all = append(all, q)
	}
	sweepAnswers()
	slices.SortFunc(all, func(a, b Question) int { return a.At.Compare(b.At) })
	return all, nil
}

// A question outlives its asker only if that process was killed, and a note
// nobody read in a day is old news.
func (q Question) expired(now time.Time) bool {
	if q.Wait {
		return now.Sub(q.At) > askTimeout+time.Minute
	}
	return now.Sub(q.At) > 24*time.Hour
}

// Answers nobody collected belong to questions whose askers are gone.
func sweepAnswers() {
	paths, _ := filepath.Glob(filepath.Join(Dir(), "answers", "*.txt"))
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && time.Since(info.ModTime()) > askTimeout {
			_ = os.Remove(p)
		}
	}
}

// Waiting questions stay until answered.
func Delivered(notes []Question) {
	for _, q := range notes {
		if !q.Wait {
			_ = os.Remove(questionPath(q.ID))
		}
	}
}

func newID() string { return strconv.FormatInt(time.Now().UnixNano(), 36) }
