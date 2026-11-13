package hq

import (
	"slices"
	"testing"
)

func TestGoalsIn(t *testing.T) {
	got := GoalsIn("# This quarter\n\n- **Ship checkout redesign**\n  - nested detail\n- [ ] Cut API latency\nSome prose\n* Hire a designer\n")
	want := []string{"This quarter", "Ship checkout redesign", "Cut API latency", "Hire a designer"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestMatchGoal(t *testing.T) {
	goals := []string{"Ship checkout redesign", "Cut API latency"}
	if g, err := MatchGoal(goals, "checkout"); err != nil || g != "Ship checkout redesign" {
		t.Fatalf("%q %v", g, err)
	}
	if _, err := MatchGoal(goals, "e"); err == nil {
		t.Fatal("an ambiguous name should be refused")
	}
}
