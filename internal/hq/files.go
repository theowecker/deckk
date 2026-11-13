package hq

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A Suggestion is the next prompt the deckk session proposes for a session.
// The deckk session writes these and the dashboard only reads them, so the
// two processes never write the same file.
type Suggestion struct {
	Prompt string    `json:"prompt"`
	Why    string    `json:"why,omitempty"`
	At     time.Time `json:"at"`
	// Safe means the deckk session judged it fine to send without review.
	// It is only sent when the user's setting allows sending.
	Safe bool `json:"safe,omitempty"`
}

func LoadSuggestions() (map[string]Suggestion, error) {
	all := map[string]Suggestion{}
	data, err := os.ReadFile(path("suggestions.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return all, nil
	} else if err != nil {
		return nil, err
	}
	return all, json.Unmarshal(data, &all)
}

func Suggest(id string, sg Suggestion) error {
	all, err := LoadSuggestions()
	if err != nil {
		return err
	}
	all[id] = sg
	return saveSuggestions(all)
}

func ClearSuggestion(id string) error {
	all, err := LoadSuggestions()
	if err != nil {
		return err
	}
	delete(all, id)
	return saveSuggestions(all)
}

func saveSuggestions(all map[string]Suggestion) error {
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path("suggestions.json"), data)
}

// An Event is something the deckk session should hear about.
type Event struct {
	At      time.Time `json:"at"`
	Session string    `json:"session"`
	Title   string    `json:"title"`
	Kind    string    `json:"kind"`
	Detail  string    `json:"detail,omitempty"`
}

const (
	Finished = "finished"
	OpenedPR = "opened a PR"
	Closed   = "closed"
)

// Line reads like the dashboard would say it: "Checkout flow finished".
func (e Event) Line() string {
	line := e.Title + " " + e.Kind
	if e.Detail != "" {
		line += ": " + e.Detail
	}
	return line
}

// The dashboard appends events and the deckk session reads past a cursor
// it keeps in its own file. Only the dashboard writes the log, so it alone
// trims it, keeping this many events once it doubles.
const inboxKeep = 400

func AppendEvents(events []Event) error {
	if len(events) == 0 {
		return nil
	}
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path("inbox.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	for _, e := range events {
		line, err := json.Marshal(e)
		if err != nil {
			f.Close()
			return err
		}
		buf.Write(append(line, '\n'))
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	all, err := readInbox()
	if err != nil || len(all) <= inboxKeep*2 {
		return err
	}
	return trimInbox(all[len(all)-inboxKeep:])
}

func readInbox() ([]Event, error) {
	f, err := os.Open(path("inbox.jsonl"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	defer f.Close()
	var events []Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		var e Event
		if json.Unmarshal(scanner.Bytes(), &e) == nil {
			events = append(events, e)
		}
	}
	return events, scanner.Err()
}

// Unread returns the events past the cursor and moves the cursor to the end.
func Unread() ([]Event, error) {
	events, err := readInbox()
	if err != nil {
		return nil, err
	}
	var cursor time.Time
	if data, err := os.ReadFile(path("inbox.cursor")); err == nil {
		_ = cursor.UnmarshalText([]byte(strings.TrimSpace(string(data))))
	}
	var unread []Event
	for _, e := range events {
		if e.At.After(cursor) {
			unread = append(unread, e)
		}
	}
	if len(unread) > 0 {
		last, err := unread[len(unread)-1].At.MarshalText()
		if err != nil {
			return nil, err
		}
		if err := writeAtomic(path("inbox.cursor"), last); err != nil {
			return nil, err
		}
	}
	return unread, nil
}

func trimInbox(keep []Event) error {
	var buf bytes.Buffer
	for _, e := range keep {
		line, err := json.Marshal(e)
		if err != nil {
			return err
		}
		buf.Write(append(line, '\n'))
	}
	return writeAtomic(path("inbox.jsonl"), buf.Bytes())
}

func ReadGoals() (string, error) {
	data, err := os.ReadFile(path("goals.md"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return string(data), err
}

func WriteGoals(text string) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	return writeAtomic(path("goals.md"), []byte(text))
}

func writeAtomic(name string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	tmp := name + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, name)
}
