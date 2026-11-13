package store

import (
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/theowecker/deckk/internal/claude"
)

type Store struct {
	path     string
	Pinned   map[string]bool      `json:"pinned"`
	Archived map[string]bool      `json:"archived"`
	Open     map[string]bool      `json:"open"`
	Names    map[string]string    `json:"names"`
	Closed   map[string]time.Time `json:"closed"`
	// Running survives a restart so sessions it kills can still be restored.
	Running map[string]bool `json:"running"`
	// Suggestions from the deckk session that were typed in or dismissed,
	// by the time each suggestion was made, so neither happens twice.
	Prefilled map[string]time.Time `json:"prefilled,omitempty"`
	Dismissed map[string]time.Time `json:"dismissed,omitempty"`
}

func DefaultPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "deckk", "state.json")
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, Pinned: map[string]bool{}, Archived: map[string]bool{}, Open: map[string]bool{}, Names: map[string]string{}, Closed: map[string]time.Time{}, Running: map[string]bool{},
		Prefilled: map[string]time.Time{}, Dismissed: map[string]time.Time{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	} else if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, err
	}
	if s.Pinned == nil {
		s.Pinned = map[string]bool{}
	}
	if s.Archived == nil {
		s.Archived = map[string]bool{}
	}
	if s.Open == nil {
		s.Open = map[string]bool{}
	}
	if s.Names == nil {
		s.Names = map[string]string{}
	}
	if s.Closed == nil {
		s.Closed = map[string]time.Time{}
	}
	if s.Running == nil {
		s.Running = map[string]bool{}
	}
	if s.Prefilled == nil {
		s.Prefilled = map[string]time.Time{}
	}
	if s.Dismissed == nil {
		s.Dismissed = map[string]time.Time{}
	}
	return s, nil
}

func (s *Store) TogglePin(id string) error {
	toggle(s.Pinned, id)
	delete(s.Archived, id)
	return s.save()
}

func (s *Store) ToggleArchive(id string) error {
	toggle(s.Archived, id)
	delete(s.Pinned, id)
	return s.save()
}

// Saves only on change, since it runs on every refresh.
func (s *Store) SetOpen(added, removed []string) error {
	changed := false
	for _, id := range added {
		if !s.Open[id] {
			s.Open[id] = true
			changed = true
		}
	}
	for _, id := range removed {
		if s.Open[id] {
			delete(s.Open, id)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.save()
}

func (s *Store) SetClosed(closed, reopened []string, now time.Time, keep time.Duration) error {
	changed := false
	for _, id := range closed {
		s.Closed[id] = now
		changed = true
	}
	for _, id := range reopened {
		if _, ok := s.Closed[id]; ok {
			delete(s.Closed, id)
			changed = true
		}
	}
	for id, at := range s.Closed {
		if now.Sub(at) > keep {
			delete(s.Closed, id)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.save()
}

func (s *Store) SetRunning(running map[string]bool) error {
	if maps.Equal(s.Running, running) {
		return nil
	}
	s.Running = maps.Clone(running)
	return s.save()
}

func (s *Store) MarkPrefilled(id string, at time.Time) error {
	s.Prefilled[id] = at
	return s.saveSuggestions(at)
}

func (s *Store) Dismiss(id string, at time.Time) error {
	s.Dismissed[id] = at
	return s.saveSuggestions(at)
}

// A suggestion goes stale once its session takes another turn, so marks on
// week-old ones only take up room.
const suggestionsFor = 7 * 24 * time.Hour

func (s *Store) saveSuggestions(now time.Time) error {
	for _, marks := range []map[string]time.Time{s.Prefilled, s.Dismissed} {
		maps.DeleteFunc(marks, func(_ string, at time.Time) bool { return now.Sub(at) > suggestionsFor })
	}
	return s.save()
}

func (s *Store) Rename(id, name string) error {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		delete(s.Names, id)
	} else {
		s.Names[id] = name
	}
	return s.save()
}

func (s *Store) ApplyNames(sessions []claude.Session) {
	for i := range sessions {
		if name, ok := s.Names[sessions[i].ID]; ok {
			sessions[i].Title = name
		}
	}
}

func toggle(set map[string]bool, id string) {
	if set[id] {
		delete(set, id)
	} else {
		set[id] = true
	}
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
