package ui

import (
	"strings"
	"testing"

	"github.com/theowecker/deckk/internal/config"

	"github.com/theowecker/deckk/internal/github"
	"github.com/theowecker/deckk/internal/hq"
)

func TestPRChangesBecomeEventsAfterTheFirstLook(t *testing.T) {
	m, host := newTestModel(t)
	m.cfg.Notify = config.NotifyOn
	i := indexOf(m, "eeeeeeee")
	s := m.flat[i]
	j := 0
	for k := range m.sessions {
		if m.sessions[k].ID == s.ID {
			j = k
		}
	}
	m.sessions[j].PR, m.sessions[j].PRURL = "#7", "https://github.com/acme/shop/pull/7"
	set := func(pr github.PR) []hq.Event {
		m.sessions[j].PRStatus = pr
		return m.trackPRs()
	}
	if ev := set(github.PR{State: "open", Checks: "pending"}); len(ev) != 0 {
		t.Fatalf("the first look is not news: %+v", ev)
	}
	ev := set(github.PR{State: "open", Checks: "failing", Failing: []string{"test"}})
	if len(ev) != 1 || ev[0].Kind != hq.ChecksFailed || !strings.Contains(ev[0].Line(), "'s PR checks failed: #7 test") {
		t.Fatalf("events %+v", ev)
	}
	if ev := set(github.PR{State: "open", Checks: "failing", Failing: []string{"test"}}); len(ev) != 0 {
		t.Fatalf("an unchanged PR is not news: %+v", ev)
	}
	run(m.notify(ev))
	if len(host.notes) != 1 || !strings.Contains(host.notes[0], "PR checks failed") {
		t.Fatalf("failing checks on a session you aren't watching should notify: %v", host.notes)
	}
	if ev := set(github.PR{State: "merged", Checks: "passing"}); len(ev) != 1 || ev[0].Kind != hq.Merged {
		t.Fatalf("events %+v", ev)
	}
}
