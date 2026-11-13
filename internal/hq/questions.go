package hq

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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

// Await polls for the deckk session's answer and cleans up either way.
func Await(id string, timeout time.Duration) (string, bool) {
	defer func() { _ = os.Remove(questionPath(id)) }()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(answerPath(id)); err == nil {
			_ = os.Remove(answerPath(id))
			return string(data), true
		}
		time.Sleep(time.Second)
	}
	return "", false
}

func Answer(id, text string) error {
	if _, err := os.Stat(questionPath(id)); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("question %s is no longer waiting", id)
	}
	if err := os.MkdirAll(filepath.Dir(answerPath(id)), 0o755); err != nil {
		return err
	}
	return writeAtomic(answerPath(id), []byte(text))
}

// Questions lists what sessions have asked or noted, oldest first.
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
		if json.Unmarshal(data, &q) == nil {
			all = append(all, q)
		}
	}
	slices.SortFunc(all, func(a, b Question) int { return a.At.Compare(b.At) })
	return all, nil
}

// Delivered removes notes once read; waiting questions stay until answered.
func Delivered(notes []Question) {
	for _, q := range notes {
		if !q.Wait {
			_ = os.Remove(questionPath(q.ID))
		}
	}
}

func newID() string { return strconv.FormatInt(time.Now().UnixNano(), 36) }
