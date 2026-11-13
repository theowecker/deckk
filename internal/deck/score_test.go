package deck

import (
	"testing"
	"time"

	"github.com/theowecker/deckk/internal/claude"
)

func TestTitlesRankAboveWhatWasSaid(t *testing.T) {
	now := time.Now()
	sessions := []claude.Session{
		{ID: "said", Title: "Refactor", LastPrompt: "fix the checkout bug", UpdatedAt: now},
		{ID: "branch", Title: "Payments", Branch: "checkout-v2", UpdatedAt: now.Add(-time.Hour)},
		{ID: "title", Title: "Checkout flow", UpdatedAt: now.Add(-2 * time.Hour)},
		{ID: "none", Title: "Docs", UpdatedAt: now},
	}
	var matched []claude.Session
	for _, s := range sessions {
		if Matches(s, "checkout") {
			matched = append(matched, s)
		}
	}
	Rank(matched, func(s claude.Session) int { return Score(s, "checkout") })
	var order []string
	for _, s := range matched {
		order = append(order, s.ID)
	}
	if len(order) != 3 || order[0] != "title" || order[1] != "branch" || order[2] != "said" {
		t.Fatalf("order %v", order)
	}
	if Score(claude.Session{Title: "Docs"}, "pagination", "moving search to cursor pagination") != 1 {
		t.Fatal("the deckk session's note should count")
	}
}
