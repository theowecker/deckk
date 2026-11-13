package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/theowecker/deckk/internal/config"
	"github.com/theowecker/deckk/internal/hq"
	"github.com/theowecker/deckk/internal/web"
)

type fakePage struct {
	last   any
	closed bool
}

func (p *fakePage) Publish(state any) { p.last = state }
func (p *fakePage) URL() string       { return "http://127.0.0.1:4747/#t=x" }
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
