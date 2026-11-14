package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/config"
	"github.com/theowecker/deckk/internal/hq"
	"github.com/theowecker/deckk/internal/web"
)

type fakePage struct {
	last   any
	closed bool
}

func (p *fakePage) Publish(state any) { p.last = state }
func (p *fakePage) URL() string       { return "http://127.0.0.1:4747/#c=x" }
func (p *fakePage) Close() error      { p.closed = true; return nil }

func TestThePageShowsNextUpAndCanDismissIt(t *testing.T) {
	m, host := newTestModel(t)
	m.cfg.Suggest = config.SuggestPrefill
	page := &fakePage{}
	m = m.WithWeb(func() (Publisher, error) { return page, nil })
	suggestInside(t, hq.Suggestion{Prompt: "run the tests", At: time.Now()})
	m = refresh(t, m, host, waiting)
	st, ok := page.last.(web.State)
	if !ok || len(st.NextUp) != 1 || st.NextUp[0].ID != "dddddddd" || !st.NextUp[0].Ready || st.Counts.Waiting == 0 {
		t.Fatalf("state %+v", st)
	}
	_, cmd := m.Update(WebAction(web.Action{Kind: web.Dismiss, ID: "dddddddd"}))
	run(cmd)
	if st := page.last.(web.State); len(st.NextUp) != 0 {
		t.Fatalf("still listed after dismiss: %+v", st.NextUp)
	}
}

func TestTheWebViewTurnsOnAndOffAtOnce(t *testing.T) {
	m, _ := newTestModel(t)
	starts := 0
	page := &fakePage{}
	m = m.WithWeb(func() (Publisher, error) { starts++; return page, nil })
	if m.web == nil || starts != 1 {
		t.Fatal("the web view is on by default")
	}
	m.setting = settingIndex(t, "web_view")
	m.settings = true
	m, _ = press(m, "enter")
	if m.web != nil || !page.closed {
		t.Fatal("turning it off should stop the server at once")
	}
	m, _ = press(m, "enter")
	if m.web == nil || starts != 2 {
		t.Fatal("turning it on should start it again")
	}
}

func TestCSaysHowToTurnTheDeckkSessionOn(t *testing.T) {
	m, host := newTestModel(t)
	opened := host.hqOpened
	m.cfg.HQ = config.Off
	m, cmd := press(m, "c")
	run(cmd)
	if host.hqOpened != opened || !strings.Contains(m.flash, "Turn it on in settings") {
		t.Fatalf("opened %d, flash %q", host.hqOpened, m.flash)
	}
}

func settingIndex(t *testing.T, key string) int {
	t.Helper()
	for i, o := range config.Options {
		if o.Key == key {
			return i
		}
	}
	t.Fatalf("no setting %s", key)
	return 0
}

func TestAConfigChangedElsewhereIsPickedUp(t *testing.T) {
	m, host := newTestModel(t)
	other, err := config.Load(m.cfg.Path())
	if err != nil {
		t.Fatal(err)
	}
	other.HQ = config.Off
	time.Sleep(10 * time.Millisecond)
	if err := other.Save(); err != nil {
		t.Fatal(err)
	}
	m.loadedAt = time.Now().Add(-time.Second)
	tm, _ := m.Update(loadedMsg{sessions: m.sessions, windows: host.windows, started: time.Now(), cfg: m.changedConfig()})
	if tm.(Model).cfg.HQOn() {
		t.Fatal("the dashboard should see the deckk session turned off")
	}
}

func TestThePageCanWriteToSessionsAndToDeckk(t *testing.T) {
	m, host := withDeckk(t)
	m = load(t, m, host, append(slices.Clone(m.sessions), deckkSession()))
	m = refresh(t, m, host, waiting)
	host.delivered = nil
	act := func(a web.Action) {
		tm, cmd := m.Update(WebAction(a))
		m = tm.(Model)
		for _, msg := range run(cmd) {
			if _, ok := msg.(deliveredMsg); ok {
				tm, _ = m.Update(msg)
				m = tm.(Model)
			}
		}
	}
	act(web.Action{Kind: web.Prompt, ID: "dddddddd", Text: "run the tests", Send: true})
	act(web.Action{Kind: web.Chat, Text: "what needs me?"})
	if len(host.delivered) != 2 || host.delivered[0] != (typing{"/dev/ttys101", "run the tests", "", true}) || host.delivered[1].target != "%9" || !host.delivered[1].send {
		t.Fatalf("delivered %v", host.delivered)
	}
	host.prompts["/dev/ttys101"] = "my own draft"
	act(web.Action{Kind: web.Prompt, ID: "dddddddd", Text: "another"})
	if !strings.Contains(m.flash, "already has text") {
		t.Fatalf("flash %q", m.flash)
	}
	act(web.Action{Kind: web.Goals, Text: "# Ship it"})
	if goals, _ := hq.ReadGoals(); goals != "# Ship it" {
		t.Fatalf("goals %q", goals)
	}
	act(web.Action{Kind: web.Pin, ID: "aaaaaaaa"})
	act(web.Action{Kind: web.Rename, ID: "aaaaaaaa", Text: "Renamed"})
	if !m.store.Pinned["aaaaaaaa"] || m.store.Names["aaaaaaaa"] != "Renamed" {
		t.Fatal("pin and rename should reach the store")
	}
}

func TestThePageClosesAWorkingSessionOnlyWhenConfirmed(t *testing.T) {
	m, host := newTestModel(t)
	tm, cmd := m.Update(WebAction(web.Action{Kind: web.Close, ID: "dddddddd"}))
	run(cmd)
	if len(host.closed) != 0 {
		t.Fatalf("closed without confirming: %v", host.closed)
	}
	_, cmd = tm.Update(WebAction(web.Action{Kind: web.Close, ID: "dddddddd", Confirm: true}))
	run(cmd)
	if !slices.Equal(host.closed, []string{"@1"}) {
		t.Fatalf("closed %v", host.closed)
	}
}

func TestThePageStartsASessionWithItsFirstPrompt(t *testing.T) {
	m, host := newTestModel(t)
	dir := t.TempDir()
	tm, cmd := m.Update(WebAction(web.Action{Kind: web.New, Folder: dir + "/fresh", Text: "set it up", Send: true}))
	run(cmd)
	if len(host.opened) != 1 || !strings.HasPrefix(host.opened[0], dir+"/fresh ") || !isDir(dir+"/fresh") {
		t.Fatalf("opened %v", host.opened)
	}
	if w := tm.(Model).deckk.awaiting; len(w) != 1 || w[0].prompt != "set it up" || !w[0].send {
		t.Fatalf("awaiting %+v", w)
	}
	_, cmd = tm.Update(WebAction(web.Action{Kind: web.New, Folder: "relative/path"}))
	run(cmd)
	if len(host.opened) != 1 {
		t.Fatalf("a relative folder should be refused: %v", host.opened)
	}
}

func TestWritingFromThePageReplacesDeckksTypedSuggestion(t *testing.T) {
	m, host := newTestModel(t)
	m.cfg.Suggest = config.SuggestPrefill
	suggestInside(t, hq.Suggestion{Prompt: "run the tests", At: time.Now()})
	m = refresh(t, m, host, waiting)
	m = refresh(t, m, host, waiting)
	if _, ok := m.typedIn(m.sessions[slices.IndexFunc(m.sessions, func(s claude.Session) bool { return s.ID == "dddddddd" })]); !ok {
		t.Fatal("the suggestion should be typed in first")
	}
	tm, cmd := m.Update(WebAction(web.Action{Kind: web.Prompt, ID: "dddddddd", Text: "run only the fast tests", Send: true}))
	for _, msg := range run(cmd) {
		tm, _ = tm.Update(msg)
	}
	last := host.delivered[len(host.delivered)-1]
	if last.text != "run only the fast tests" || last.replace != "run the tests" {
		t.Fatalf("delivered %+v", last)
	}
	if _, kept := tm.(Model).store.Prefilled["dddddddd"]; kept {
		t.Fatal("deckk's copy should be forgotten once the user's text replaces it")
	}
}

func TestASettingChangedDuringALoadIsntUndoneByIt(t *testing.T) {
	m, host := newTestModel(t)
	started := time.Now()
	stale := *m.cfg
	m.setting = settingIndex(t, "notifications")
	m.settings = true
	m, _ = press(m, "enter")
	changed := m.cfg.Notify
	tm, _ := m.Update(loadedMsg{sessions: m.sessions, windows: host.windows, started: started, deckk: m.snapshot(), cfg: &stale})
	if got := tm.(Model).cfg.Notify; got != changed {
		t.Fatalf("notifications went back to %q", got)
	}
}

func TestAReviewShowsOnTheBoardAndThePage(t *testing.T) {
	m, _ := newTestModel(t)
	page := &fakePage{}
	m = m.WithWeb(func() (Publisher, error) { return page, nil })
	s, _ := m.sessionByID("dddddddd")
	m.reviewing[s.ID] = true
	tm, _ := m.Update(reviewDoneMsg{session: s, review: hq.Review{At: time.Now(), Text: "- debug print left in api.go\n- no test for retries"}})
	m = tm.(Model)
	if m.reviewing[s.ID] || !strings.Contains(m.flash, "found 2 concerns") {
		t.Fatalf("reviewing %v, flash %q", m.reviewing, m.flash)
	}
	if recent, _ := hq.Recent(5); len(recent) == 0 || recent[0].Kind != hq.Reviewed || !strings.Contains(recent[0].Detail, "found 2 concerns") {
		t.Fatalf("the deckk session and the page's activity should hear of the review: %+v", recent)
	}
	if band := tabInfo(s, time.Now(), m.bandInfo(s, false)); !strings.Contains(band, "⚑ review found 2 concerns") {
		t.Fatalf("the session's band should show the review: %q", band)
	}
	mark, line, ok := m.reviewSummary(s)
	if !ok || !strings.Contains(mark, "⚠") || !strings.Contains(line, "debug print left in api.go") || !strings.Contains(line, "+1 more") {
		t.Fatalf("mark %q line %q", mark, line)
	}
	st := page.last.(web.State)
	for _, g := range st.Groups {
		for _, ws := range g.Sessions {
			if ws.ID == s.ID && (ws.Review == nil || ws.Review.OK || len(ws.Review.Concerns) != 2) {
				t.Fatalf("page review %+v", ws.Review)
			}
		}
	}
}

func TestReviewsInDoneModeWaitForTheWorkToSoundFinished(t *testing.T) {
	m, _ := newTestModel(t)
	m.cfg.Reviews = ""
	s, _ := m.sessionByID("dddddddd")
	i := slices.IndexFunc(m.sessions, func(x claude.Session) bool { return x.ID == s.ID })
	m.sessions[i].Git.Changed = 3

	m.sessions[i].LastReply = "Which of the two date formats should the parser accept?"
	m.autoReview([]hq.Event{{Session: s.ID, Kind: hq.Finished}})
	if m.reviewing[s.ID] || len(m.reviewQueue) != 0 {
		t.Fatal("a question isn't finished work")
	}
	m.sessions[i].LastReply = "All tests pass. Ready to commit."
	m.autoReview([]hq.Event{{Session: s.ID, Kind: hq.Finished}})
	if !m.reviewing[s.ID] {
		t.Fatal("finished work with changes should be reviewed")
	}
}

func TestEShowsTheWholeReviewThenRunsAFreshOne(t *testing.T) {
	m, _ := newTestModel(t)
	m.width, m.height = 120, 40
	m = selectID(m, "dddddddd")
	s, _ := m.selected()
	m.deckk.reviews[s.ID] = hq.Review{At: time.Now(), Text: "- debug print left in api.go\n- no test for retries\n- TODO in handler"}
	m, cmd := press(m, "e")
	if cmd != nil || m.reviewing[s.ID] {
		t.Fatal("the first e should only show the review")
	}
	shown := strings.Join(m.preview(s), "\n")
	for _, want := range []string{"The review flagged", "debug print left in api.go", "no test for retries", "TODO in handler", "e reviews again"} {
		if !strings.Contains(shown, want) {
			t.Fatalf("preview lacks %q:\n%s", want, shown)
		}
	}
	m, cmd = press(m, "e")
	if cmd == nil || !m.reviewing[s.ID] {
		t.Fatal("the second e should run a fresh review")
	}
}

func TestAReviewWithConcernsNotifiesUnlessYoureWatching(t *testing.T) {
	m, host := newTestModel(t)
	m.cfg.Notify = config.NotifyOn
	elsewhere, _ := m.sessionByID("eeeeeeee")
	flagged := hq.Review{At: time.Now(), Text: "- debug print left in"}
	run(m.notifyReview(reviewDoneMsg{session: elsewhere, review: flagged}))
	if len(host.notes) != 1 || !strings.Contains(host.notes[0], "review found concerns") || !strings.Contains(host.notes[0], "1 concern: debug print left in") {
		t.Fatalf("notes %v", host.notes)
	}
	run(m.notifyReview(reviewDoneMsg{session: elsewhere, review: hq.Review{At: time.Now(), OK: true}}))
	if len(host.notes) != 1 {
		t.Fatalf("a clean review is no news: %v", host.notes)
	}
}

func TestManualReviewsNeverStartOnTheirOwn(t *testing.T) {
	m, _ := newTestModel(t)
	m.cfg.Reviews = config.ReviewManual
	s, _ := m.sessionByID("dddddddd")
	for i := range m.sessions {
		if m.sessions[i].ID == s.ID {
			m.sessions[i].Git.Changed, m.sessions[i].LastReply = 3, "All tests pass. Ready to commit."
		}
	}
	if cmd := m.autoReview([]hq.Event{{Session: s.ID, Kind: hq.Finished}, {Session: s.ID, Kind: hq.OpenedPR}}); cmd != nil || m.reviewing[s.ID] {
		t.Fatal("manual means only when asked")
	}
}

func TestNotificationsStayOffUnlessTurnedOn(t *testing.T) {
	m, host := newTestModel(t)
	elsewhere, _ := m.sessionByID("eeeeeeee")
	run(m.notify([]hq.Event{{Session: elsewhere.ID, Kind: hq.Finished}}))
	run(m.notifyReview(reviewDoneMsg{session: elsewhere, review: hq.Review{Text: "- concern"}}))
	if len(host.notes) != 0 {
		t.Fatalf("notifications are opt-in: %v", host.notes)
	}
}
