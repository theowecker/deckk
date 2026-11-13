package ui

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/config"
	"github.com/theowecker/deckk/internal/deck"
	"github.com/theowecker/deckk/internal/hq"
	"github.com/theowecker/deckk/internal/tmux"
)

// withDeckk adds a running, idle deckk session in its own pane.
func withDeckk(t *testing.T) (Model, *fakeHost) {
	t.Helper()
	m, host := newTestModel(t)
	host.hqPane = tmux.Pane{ID: "%9", Role: "hq"}
	if err := m.store.SetBriefed(time.Now()); err != nil {
		t.Fatal(err)
	}
	return m, host
}

func deckkSession() claude.Session {
	return claude.Session{ID: "hqhqhqhq", Title: "deckk", Cwd: hq.Dir(), PID: 9, TTY: "/dev/ttys109", Status: "idle"}
}

// refresh is a load where sessions changed, with its effects played back.
func refresh(t *testing.T, m Model, host *fakeHost, change func([]claude.Session)) Model {
	t.Helper()
	sessions := slices.Clone(m.sessions)
	if m.deckk.running {
		sessions = append(sessions, m.deckk.session)
	}
	if change != nil {
		change(sessions)
	}
	return load(t, m, host, sessions)
}

// load is a refresh that finds exactly these sessions.
func load(t *testing.T, m Model, host *fakeHost, sessions []claude.Session) Model {
	t.Helper()
	tm, cmd := m.Update(loadedMsg{sessions: sessions, windows: host.windows, started: time.Now(), deckk: m.snapshot(), edits: host.edits})
	for _, msg := range run(cmd) {
		if _, ok := msg.(deliveredMsg); ok {
			tm, _ = tm.Update(msg)
		}
	}
	return tm.(Model)
}

// inside is the test session hosted in deckk's strip.
func inside(sessions []claude.Session) *claude.Session {
	for i := range sessions {
		if sessions[i].ID == "dddddddd" {
			return &sessions[i]
		}
	}
	return nil
}

func TestTheDeckkSessionIsNotListedWithTheRest(t *testing.T) {
	m, host := withDeckk(t)
	m = refresh(t, m, host, nil)
	tm, _ := m.Update(loadedMsg{sessions: append(slices.Clone(m.sessions), deckkSession()), windows: host.windows, started: time.Now()})
	m = tm.(Model)
	if !m.deckk.running || slices.ContainsFunc(m.flat, func(s claude.Session) bool { return s.ID == "hqhqhqhq" }) {
		t.Fatalf("running %v, listed %v", m.deckk.running, m.flat)
	}
}

func TestAFinishedSessionReachesTheDeckkSessionOnce(t *testing.T) {
	m, host := withDeckk(t)
	tm, _ := m.Update(loadedMsg{sessions: append(slices.Clone(m.sessions), deckkSession()), windows: host.windows, started: time.Now()})
	m = tm.(Model)

	m = refresh(t, m, host, func(s []claude.Session) {
		in := inside(s)
		in.Status, in.StatusAt, in.LastReply = "idle", time.Now(), "Done: the cart total is fixed."
	})
	if len(host.delivered) != 1 {
		t.Fatalf("delivered %v", host.delivered)
	}
	d := host.delivered[0]
	if d.target != "%9" || !d.send || !strings.Contains(d.text, "[deckk] Inside deckk finished") {
		t.Fatalf("nudge %+v", d)
	}
	events, err := hq.Unread()
	if err != nil || len(events) != 1 || events[0].Kind != hq.Finished || !strings.Contains(events[0].Detail, "cart total") {
		t.Fatalf("inbox %v %v", events, err)
	}

	m = refresh(t, m, host, nil)
	if len(host.delivered) != 1 {
		t.Fatalf("nudged again with no news: %v", host.delivered)
	}
}

func TestNoNudgeWhileYouAreTalkingToTheDeckkSession(t *testing.T) {
	m, host := withDeckk(t)
	host.hqPane.Seen = true
	tm, _ := m.Update(loadedMsg{sessions: append(slices.Clone(m.sessions), deckkSession()), windows: host.windows, started: time.Now()})
	m = tm.(Model)
	m = refresh(t, m, host, func(s []claude.Session) { inside(s).Status = "idle" })
	if len(host.delivered) != 0 {
		t.Fatalf("typed into the pane you're using: %v", host.delivered)
	}
}

func waiting(s []claude.Session) {
	in := inside(s)
	in.Status, in.StatusAt = "idle", time.Now().Add(-time.Minute)
}

func TestASuggestionIsTypedIntoAWaitingSessionOnce(t *testing.T) {
	m, host := newTestModel(t)
	m = refresh(t, m, host, waiting)
	suggestInside(t, hq.Suggestion{Prompt: "run the tests again", Why: "the API merged", At: time.Now(), Safe: true})
	m = refresh(t, m, host, waiting)
	if len(host.delivered) != 1 || host.delivered[0] != (typing{"/dev/ttys101", "run the tests again", "", false}) {
		t.Fatalf("delivered %v", host.delivered)
	}
	if len(m.groups) == 0 || m.groups[0].Section != deck.NextUp || m.groups[0].Sessions[0].ID != "dddddddd" {
		t.Fatalf("groups %v", m.groups)
	}
	if !strings.Contains(m.suggestionRow(m.groups[0].Sessions[0], false, 20), "ready") {
		t.Fatal("a typed-in suggestion should say it's ready")
	}
	m = refresh(t, m, host, waiting)
	if len(host.delivered) != 1 {
		t.Fatalf("typed twice: %v", host.delivered)
	}
}

func TestASuggestionWaitsForAnEmptyPrompt(t *testing.T) {
	m, host := newTestModel(t)
	host.prompts = map[string]string{"/dev/ttys101": "something of mine"}
	suggestInside(t, hq.Suggestion{Prompt: "run the tests", At: time.Now()})
	m = refresh(t, m, host, waiting)
	if len(host.delivered) != 0 || m.prefilled(*inside(m.sessions), m.deckk.suggestions["dddddddd"]) {
		t.Fatalf("typed over the user's text: %v", host.delivered)
	}
}

func TestNothingIsTypedIntoTheTabYouAreOn(t *testing.T) {
	m, host := newTestModel(t)
	host.windows[1].Active = true
	suggestInside(t, hq.Suggestion{Prompt: "run the tests", At: time.Now()})
	m = refresh(t, m, host, waiting)
	if len(host.delivered) != 0 {
		t.Fatalf("typed into the visible tab: %v", host.delivered)
	}
	m.cursor = indexOf(m, "dddddddd")
	tm, cmd := m.Update(keyMsg("enter"))
	_ = tm
	run(cmd)
	if len(host.delivered) != 1 {
		t.Fatalf("enter on a session should bring its suggestion along: %v", host.delivered)
	}
}

func TestSuggestionsAreSentOnlyWhenAllowedAndSafe(t *testing.T) {
	for _, c := range []struct {
		setting string
		safe    bool
		send    bool
	}{
		{config.SuggestPrefill, true, false},
		{config.SuggestSend, false, false},
		{config.SuggestSend, true, true},
	} {
		m, host := newTestModel(t)
		m.cfg.Suggest = c.setting
		suggestInside(t, hq.Suggestion{Prompt: "commit it", At: time.Now(), Safe: c.safe})
		refresh(t, m, host, waiting)
		if len(host.delivered) != 1 || host.delivered[0].send != c.send {
			t.Errorf("%q safe=%v: delivered %v", c.setting, c.safe, host.delivered)
		}
	}
}

func TestListOnlyKeepsSuggestionsOutOfPrompts(t *testing.T) {
	m, host := newTestModel(t)
	m.cfg.Suggest = config.SuggestList
	suggestInside(t, hq.Suggestion{Prompt: "commit it", At: time.Now()})
	m = refresh(t, m, host, waiting)
	if len(host.delivered) != 0 || m.groups[0].Section != deck.NextUp {
		t.Fatalf("delivered %v, groups %v", host.delivered, m.groups)
	}
}

func TestASuggestionGoesStaleOnceTheSessionMovesOn(t *testing.T) {
	m, host := newTestModel(t)
	suggestInside(t, hq.Suggestion{Prompt: "commit it", At: time.Now().Add(-time.Hour)})
	m = refresh(t, m, host, waiting)
	if len(m.groups) > 0 && m.groups[0].Section == deck.NextUp {
		t.Fatal("a suggestion older than the session's last turn is stale")
	}
}

func TestDismissingASuggestion(t *testing.T) {
	m, host := newTestModel(t)
	m.cfg.Suggest = config.SuggestList
	suggestInside(t, hq.Suggestion{Prompt: "commit it", At: time.Now()})
	m = refresh(t, m, host, waiting)
	m.cursor = 0
	m, _ = press(m, "d")
	m = refresh(t, m, host, waiting)
	if m.groups[0].Section == deck.NextUp {
		t.Fatal("a dismissed suggestion should leave Next up")
	}
}

func TestSessionsDeckkStartsCanAskIt(t *testing.T) {
	m, host := newTestModel(t)
	m.cfg.Ask = config.On
	if err := hq.Setup("/bin/deckk"); err != nil {
		t.Fatal(err)
	}
	m = selectID(m, "aaaaaaaa")
	_, cmd := press(m, "enter")
	run(cmd)
	if len(host.opened) != 1 || !strings.Contains(host.opened[0], "--mcp-config="+filepath.Join(hq.Dir(), "mcp.json")) {
		t.Fatalf("opened %v", host.opened)
	}
}

func TestPressingCOpensTheDeckkSession(t *testing.T) {
	m, host := newTestModel(t)
	before := host.hqOpened
	_, cmd := press(m, "c")
	run(cmd)
	if host.hqOpened != before+1 {
		t.Fatal("c should open the deckk session")
	}
}

func suggestInside(t *testing.T, sg hq.Suggestion) {
	t.Helper()
	suggestFor(t, "dddddddd", sg)
}

func suggestFor(t *testing.T, id string, sg hq.Suggestion) {
	t.Helper()
	if err := hq.Suggest(id, sg); err != nil {
		t.Fatal(err)
	}
}

func TestTheDeckkSessionStartsOnceAClientIsAttached(t *testing.T) {
	m, host := newTestModel(t)
	tm, _ := m.Update(deckkStartedMsg{err: tmux.ErrDetached})
	m = refresh(t, tm.(Model), host, nil)
	if host.hqOpened != 2 {
		t.Fatalf("a detached start should be retried, opened %d", host.hqOpened)
	}
	tm, _ = m.Update(deckkStartedMsg{})
	refresh(t, tm.(Model), host, nil)
	if host.hqOpened != 2 {
		t.Fatal("a closed deckk session shouldn't come back on its own")
	}
}

func TestAQuestionReachesTheDeckkSessionRightAway(t *testing.T) {
	m, host := withDeckk(t)
	tm, _ := m.Update(loadedMsg{sessions: append(slices.Clone(m.sessions), deckkSession()), windows: host.windows, started: time.Now()})
	m = tm.(Model)
	m.deckk.nudgedAt = time.Now().Add(-20 * time.Second)
	if err := hq.Post(hq.Question{ID: "q1", Title: "Checkout", Text: "tests pass, what next?", Wait: true, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	m = refresh(t, m, host, nil)
	if len(host.delivered) != 1 || !strings.Contains(host.delivered[0].text, "Checkout asks: tests pass, what next?") {
		t.Fatalf("delivered %v", host.delivered)
	}
	refresh(t, m, host, nil)
	if len(host.delivered) != 1 {
		t.Fatal("a question should only be passed on once")
	}
}

func TestANewerSuggestionReplacesTheOneTypedIn(t *testing.T) {
	m, host := newTestModel(t)
	suggestInside(t, hq.Suggestion{Prompt: "run the tests", At: time.Now()})
	m = refresh(t, m, host, waiting)
	suggestInside(t, hq.Suggestion{Prompt: "open the PR", At: time.Now().Add(time.Second)})
	refresh(t, m, host, waiting)
	if len(host.delivered) != 2 || host.delivered[1].replace != "run the tests" || host.prompts["/dev/ttys101"] != "open the PR" {
		t.Fatalf("delivered %v, prompt %q", host.delivered, host.prompts["/dev/ttys101"])
	}
}

func TestAWithdrawnSuggestionIsTakenBackOut(t *testing.T) {
	m, host := newTestModel(t)
	suggestInside(t, hq.Suggestion{Prompt: "run the tests", At: time.Now()})
	m = refresh(t, m, host, waiting)
	if err := hq.ClearSuggestion("dddddddd"); err != nil {
		t.Fatal(err)
	}
	m = refresh(t, m, host, waiting)
	if host.prompts["/dev/ttys101"] != "" {
		t.Fatalf("prompt still holds %q", host.prompts["/dev/ttys101"])
	}
	if _, kept := m.store.Prefilled["dddddddd"]; kept {
		t.Fatal("a withdrawn suggestion should be forgotten")
	}
}

func TestDismissingTakesTheSuggestionOutOfThePrompt(t *testing.T) {
	m, host := newTestModel(t)
	suggestInside(t, hq.Suggestion{Prompt: "run the tests", At: time.Now()})
	m = refresh(t, m, host, waiting)
	m = selectID(m, "dddddddd")
	_, cmd := press(m, "d")
	run(cmd)
	if host.prompts["/dev/ttys101"] != "" {
		t.Fatalf("prompt still holds %q", host.prompts["/dev/ttys101"])
	}
}

func TestEditedSuggestionsAreLeftAlone(t *testing.T) {
	m, host := newTestModel(t)
	suggestInside(t, hq.Suggestion{Prompt: "run the tests", At: time.Now()})
	m = refresh(t, m, host, waiting)
	host.prompts["/dev/ttys101"] = "run the tests and the linter"
	_ = hq.ClearSuggestion("dddddddd")
	refresh(t, m, host, waiting)
	if host.prompts["/dev/ttys101"] != "run the tests and the linter" {
		t.Fatal("deckk should never clear text the user changed")
	}
}

func TestAFailedDeliveryIsTriedAgain(t *testing.T) {
	m, host := newTestModel(t)
	suggestInside(t, hq.Suggestion{Prompt: "run the tests", At: time.Now()})
	m.deckk.inflight["dddddddd"] = true
	tm, _ := m.Update(deliveredMsg{delivery: delivery{key: "dddddddd"}, err: errors.New("pane gone")})
	refresh(t, tm.(Model), host, waiting)
	if len(host.delivered) != 1 {
		t.Fatalf("a failed delivery should not block the next: %v", host.delivered)
	}
}

func TestNoBacklogWhileUpdatesAreOff(t *testing.T) {
	m, host := withDeckk(t)
	m.cfg.Updates = config.Off
	m = refresh(t, m, host, func(s []claude.Session) { inside(s).Status = "idle" })
	if len(m.deckk.pending) != 0 {
		t.Fatalf("pending %v", m.deckk.pending)
	}
}

func tmuxWindow(id, tty string) tmux.Window { return tmux.Window{ID: id, TTY: tty, Session: "deckk"} }

func TestOpeningDeckkAfterABreakAsksForABriefingOnce(t *testing.T) {
	m, host := withDeckk(t)
	if err := m.store.SetBriefed(time.Now().Add(-5 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	m = load(t, m, host, append(slices.Clone(m.sessions), deckkSession()))
	if len(host.delivered) != 1 || !strings.Contains(host.delivered[0].text, "[deckk] Briefing") || !host.delivered[0].send {
		t.Fatalf("delivered %v", host.delivered)
	}
	m.deckk.nudgedAt = time.Time{}
	refresh(t, m, host, nil)
	if len(host.delivered) != 1 {
		t.Fatalf("briefed twice: %v", host.delivered)
	}
}

func TestNoBriefingRightAfterTheLastOne(t *testing.T) {
	m, host := withDeckk(t)
	load(t, m, host, append(slices.Clone(m.sessions), deckkSession()))
	if len(host.delivered) != 0 {
		t.Fatalf("delivered %v", host.delivered)
	}
}
