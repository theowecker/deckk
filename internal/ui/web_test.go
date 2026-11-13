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
	if m.reviewing[s.ID] || !strings.Contains(m.flash, "found concerns") {
		t.Fatalf("reviewing %v, flash %q", m.reviewing, m.flash)
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
