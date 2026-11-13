package ui

import (
	"strings"
	"testing"

	"github.com/theowecker/deckk/internal/github"
	"github.com/theowecker/deckk/internal/hq"
	"github.com/theowecker/deckk/internal/web"
)

func TestGoalsShowTheirSessionsProgress(t *testing.T) {
	m, _ := newTestModel(t)
	page := &fakePage{}
	m = m.WithWeb(func() (Publisher, error) { return page, nil })
	m.deckk.goals = "- Ship checkout redesign\n- Cut API latency\n"
	tm, _ := m.Update(WebAction(web.Action{Kind: web.LinkGoal, ID: "dddddddd", Text: "checkout"}))
	m = tm.(Model)
	if links, _ := hq.LoadLinks(); links["dddddddd"] != "Ship checkout redesign" {
		t.Fatalf("links %v", links)
	}
	for i := range m.sessions {
		if m.sessions[i].ID == "dddddddd" {
			m.sessions[i].PR, m.sessions[i].PRStatus = "#9", github.PR{State: "open", Checks: "failing"}
		}
	}
	goals := m.goals()
	if len(goals) != 2 || len(goals[0].sessions) != 1 || len(goals[1].sessions) != 0 {
		t.Fatalf("goals %+v", goals)
	}
	if got := m.summary(goals[0]); !strings.Contains(got, "1 session") || !strings.Contains(got, "1 PR needs you") {
		t.Fatalf("summary %q", got)
	}
	if got := m.summary(goals[1]); got != "no sessions linked yet" {
		t.Fatalf("summary %q", got)
	}
	m.width = 120
	if board := strings.Join(m.goalLines(), "\n"); !strings.Contains(board, "Ship checkout redesign") || !strings.Contains(board, "1 session") {
		t.Fatalf("board %q", board)
	}
}
