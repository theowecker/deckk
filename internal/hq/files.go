package hq

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// The deckk session writes these and the dashboard only reads them, so the two
// processes never write the same file.
type Suggestion struct {
	Prompt string    `json:"prompt"`
	Why    string    `json:"why,omitempty"`
	At     time.Time `json:"at"`
	// Safe suggestions are only sent when the user's setting allows sending.
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
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, err
	}
	return all, nil
}

// The deckk session can call suggest several times at once.
func Suggest(id string, sg Suggestion) error {
	return changeSuggestions(func(all map[string]Suggestion) { all[id] = sg })
}

func ClearSuggestion(id string) error {
	return changeSuggestions(func(all map[string]Suggestion) { delete(all, id) })
}

func changeSuggestions(change func(map[string]Suggestion)) error {
	return locked(func() error {
		all, err := LoadSuggestions()
		if err != nil {
			return err
		}
		change(all)
		return saveSuggestions(all)
	})
}

func saveSuggestions(all map[string]Suggestion) error {
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path("suggestions.json"), data)
}

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
	Reviewed = "was reviewed"
)

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
	return locked(func() error { return appendEvents(events) })
}

func appendEvents(events []Event) error {
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

func Unread() ([]Event, error) {
	var unread []Event
	err := locked(func() (err error) {
		unread, err = unreadEvents()
		return err
	})
	return unread, err
}

func unreadEvents() ([]Event, error) {
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

// A reader never sees half a file, and each write gets its own temp file, so
// concurrent writers can't clash.
func writeAtomic(name string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(name), "."+filepath.Base(name)+".*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), name)
}

// locked serializes read-modify-writes across every deckk process, since the
// dashboard and several MCP servers share these files.
func locked(fn func() error) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path(".lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	return fn()
}

// Recent doesn't move the cursor.
func Recent(n int) ([]Event, error) {
	events, err := readInbox()
	if err != nil {
		return nil, err
	}
	events = events[max(len(events)-n, 0):]
	slices.Reverse(events)
	return events, nil
}
