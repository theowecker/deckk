package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theowecker/deckk/internal/claude"
)

func TestOpenSetPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetOpen([]string{"a", "b"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOpen(nil, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Open["a"] || !reopened.Open["b"] {
		t.Fatalf("open = %v", reopened.Open)
	}
}

func TestRenameOverridesAndClears(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	sessions := []claude.Session{{ID: "a", Title: "from claude"}}
	if err := s.Rename("a", "  my   name "); err != nil {
		t.Fatal(err)
	}
	s.ApplyNames(sessions)
	if sessions[0].Title != "my name" {
		t.Fatalf("title %q", sessions[0].Title)
	}
	if err := s.Rename("a", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Names["a"]; ok {
		t.Fatal("an empty name should clear the override")
	}
}

func TestPinAndArchiveAreExclusiveAndPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deckk", "state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.TogglePin("a"); err != nil {
		t.Fatal(err)
	}
	if err := s.ToggleArchive("a"); err != nil {
		t.Fatal(err)
	}
	if err := s.TogglePin("b"); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Pinned["a"] || !reopened.Archived["a"] || !reopened.Pinned["b"] {
		t.Fatalf("unexpected state: pinned=%v archived=%v", reopened.Pinned, reopened.Archived)
	}

	if err := reopened.TogglePin("b"); err != nil {
		t.Fatal(err)
	}
	if reopened.Pinned["b"] {
		t.Fatal("second toggle should unpin")
	}
}

func TestOldSuggestionMarksArePruned(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	_ = s.Dismiss("old", now.Add(-30*24*time.Hour))
	if err := s.MarkPrefilled("new", Prefill{At: now, Text: "go"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Dismissed["old"]; ok || s.Prefilled["new"].Text != "go" {
		t.Fatalf("dismissed %v prefilled %v", s.Dismissed, s.Prefilled)
	}
}

func TestPrefillsFromTheFirstVersionStillLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"prefilled":{"a":"2026-11-13T15:26:29Z","b":{"at":"2026-11-13T15:26:29Z","text":"go"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Prefilled["a"].At.IsZero() || s.Prefilled["b"].Text != "go" {
		t.Fatalf("prefilled %v", s.Prefilled)
	}
}
