package deck

import (
	"testing"
	"time"

	"github.com/theowecker/deckk/internal/claude"
)

var now = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func sessions() []claude.Session {
	return []claude.Session{
		{ID: "old", Title: "Old idea", Cwd: "/dev/app", UpdatedAt: now.Add(-48 * time.Hour)},
		{ID: "new", Title: "New idea", Cwd: "/dev/app", Branch: "feat/search", UpdatedAt: now.Add(-time.Hour)},
		{ID: "live", Title: "Live work", Cwd: "/dev/api", PID: 42, UpdatedAt: now.Add(-72 * time.Hour)},
		{ID: "busy", Title: "Busy work", Cwd: "/dev/api", PID: 43, UpdatedAt: now},
		{ID: "pin", Title: "Pinned thing", Cwd: "/dev/api", UpdatedAt: now.Add(-96 * time.Hour)},
		{ID: "gone", Title: "Archived", Cwd: "/dev/api", UpdatedAt: now},
	}
}

var marks = Marks{
	Pinned:   map[string]bool{"pin": true, "live": true},
	Archived: map[string]bool{"gone": true},
}

func TestArrangeGroupsAndSorts(t *testing.T) {
	ordered := marks
	ordered.Order = map[string]int{"live": 0, "busy": 1}
	got := ids(Arrange(sessions(), ordered, "", false))
	want := []string{"RUNNING:live", "RUNNING:busy", "PINNED:pin", "RECENT:new", "RECENT:old"}
	assertSlice(t, got, want)

	got = ids(Arrange(sessions(), ordered, "", true))
	assertSlice(t, got, append(want, "ARCHIVED:gone"))
}

func TestArrangeSeparatesSessionsOutsideDeckk(t *testing.T) {
	hosted := marks
	hosted.Hosted = map[string]bool{"busy": true}
	got := ids(Arrange(sessions(), hosted, "", false))
	assertSlice(t, got, []string{"RUNNING:busy", "IN OTHER TABS:live", "PINNED:pin", "RECENT:new", "RECENT:old"})
}

func TestRecentlyClosedSectionAndRestore(t *testing.T) {
	closed := marks
	closed.Now = now
	closed.Closed = map[string]time.Time{"old": now.Add(-time.Hour), "new": now.Add(-48 * time.Hour)}
	assertSlice(t, ids(Arrange(sessions(), closed, "", false)),
		[]string{"RUNNING:busy", "RUNNING:live", "RECENTLY CLOSED:old", "PINNED:pin", "RECENT:new"})

	var restore []string
	for _, s := range ToRestore(sessions(), closed) {
		restore = append(restore, s.ID)
	}
	assertSlice(t, restore, []string{"old"})
}

func TestArrangeByProjectPutsBusyProjectsFirst(t *testing.T) {
	groups := ArrangeByProject(sessions(), marks, "", false)
	var titles []string
	for _, g := range groups {
		titles = append(titles, g.Title)
	}
	assertSlice(t, titles, []string{"API", "APP"})
	if groups[0].Sessions[0].ID != "busy" {
		t.Fatalf("running sessions should lead their project, got %s", groups[0].Sessions[0].ID)
	}
}

func TestCollapseKeepsRunningSessions(t *testing.T) {
	g := Group{Sessions: sessions()}
	g.Collapse(1)
	var got []string
	for _, s := range g.Sessions {
		got = append(got, s.ID)
	}
	assertSlice(t, got, []string{"old", "live", "busy"})
	if g.Hidden != 3 {
		t.Fatalf("hidden %d", g.Hidden)
	}
}

func TestRestoreSkipsBackgroundSessions(t *testing.T) {
	list := sessions()
	list[0].Background = true
	withOpen := marks
	withOpen.Open = map[string]bool{"old": true}
	if got := ToRestore(list, withOpen); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestArrangeFiltersOnEveryWord(t *testing.T) {
	assertSlice(t, ids(Arrange(sessions(), marks, "app search", false)), []string{"RECENT:new"})
	assertSlice(t, ids(Arrange(sessions(), marks, "IDEA", false)), []string{"RECENT:new", "RECENT:old"})
}

func TestToRestoreCoversLastOpenButNotPins(t *testing.T) {
	withOpen := marks
	withOpen.Open = map[string]bool{"old": true, "busy": true, "gone": true}
	var got []string
	for _, s := range ToRestore(sessions(), withOpen) {
		got = append(got, s.ID)
	}
	assertSlice(t, got, []string{"old"})
}

func TestAgo(t *testing.T) {
	cases := map[time.Duration]string{
		10 * time.Second:    "now",
		5 * time.Minute:     "5m",
		3 * time.Hour:       "3h",
		50 * time.Hour:      "2d",
		10 * 24 * time.Hour: "Oct 30",
	}
	for d, want := range cases {
		if got := Ago(now.Add(-d), now); got != want {
			t.Errorf("Ago(%s) = %s, want %s", d, got, want)
		}
	}
}

func ids(groups []Group) []string {
	var out []string
	for _, g := range groups {
		for _, s := range g.Sessions {
			out = append(out, g.Section.String()+":"+s.ID)
		}
	}
	return out
}

func assertSlice(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
