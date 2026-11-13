package ui

import (
	"testing"
	"time"

	"github.com/theowecker/deckk/internal/hq"
	"github.com/theowecker/deckk/internal/web"
)

type fakePage struct{ last any }

func (p *fakePage) Publish(state any) { p.last = state }
func (p *fakePage) URL() string       { return "http://localhost:4747/?t=x" }

func TestThePageShowsNextUpAndCanDismissIt(t *testing.T) {
	m, host := newTestModel(t)
	page := &fakePage{}
	m = m.WithWeb(page)
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
