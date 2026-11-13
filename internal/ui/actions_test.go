package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/config"
	"github.com/theowecker/deckk/internal/deck"
	"github.com/theowecker/deckk/internal/store"
	"github.com/theowecker/deckk/internal/tmux"
)

func TestEnterSelectsTheWindowOfASessionInsideDeckk(t *testing.T) {
	m, host := newTestModel(t)
	m = selectID(m, "dddddddd")
	_, cmd := press(m, "enter")
	run(cmd)
	if !slices.Equal(host.selected, []string{"@1"}) || len(host.opened) != 0 {
		t.Fatalf("selected %v, opened %v", host.selected, host.opened)
	}
}

func TestEnterResumesAStoppedSessionInANewWindow(t *testing.T) {
	m, host := newTestModel(t)
	m = selectID(m, "aaaaaaaa")
	_, cmd := press(m, "enter")
	run(cmd)
	if len(host.opened) != 1 || !strings.HasSuffix(host.opened[0], "claude --resume aaaaaaaa") {
		t.Fatalf("opened %v", host.opened)
	}
}

func TestClosingAWorkingSessionNeedsConfirmation(t *testing.T) {
	m, host := newTestModel(t)
	m = selectID(m, "dddddddd")

	m, cmd := press(m, "x")
	run(cmd)
	if len(host.closed) != 0 || m.confirm == "" {
		t.Fatalf("first x should only ask, closed %v", host.closed)
	}

	_, cmd = press(m, "x")
	run(cmd)
	if !slices.Equal(host.closed, []string{"@1"}) {
		t.Fatalf("second x should close, closed %v", host.closed)
	}
}

func TestMovingASessionIntoDeckkReleasesItThenResumes(t *testing.T) {
	m, host := newTestModel(t)
	m = selectID(m, "eeeeeeee")
	_, cmd := press(m, "m")
	run(cmd)
	if !slices.Equal(host.released, []string{"/dev/ttys200"}) {
		t.Fatalf("released %v", host.released)
	}
	if len(host.opened) != 1 || !strings.HasSuffix(host.opened[0], "claude --resume eeeeeeee") {
		t.Fatalf("opened %v", host.opened)
	}
}

func TestMoveAllSkipsWorkingSessions(t *testing.T) {
	m, host := newTestModel(t)
	for i := range m.sessions {
		if m.sessions[i].ID == "eeeeeeee" {
			m.sessions[i].Status = "busy"
		}
	}
	_, cmd := press(m, "M")
	run(cmd)
	if len(host.released) != 0 {
		t.Fatalf("a working session should not be moved, released %v", host.released)
	}
}

func TestClosingSessionsOutsideDeckkIsRefused(t *testing.T) {
	m, host := newTestModel(t)
	m = selectID(m, "eeeeeeee")
	m, cmd := press(m, "x")
	run(cmd)
	if len(host.closed) != 0 || m.flash == "" {
		t.Fatalf("closed %v, flash %q", host.closed, m.flash)
	}
}

func TestLabelsCoverDashboardAndSessionWindows(t *testing.T) {
	_, host := newTestModel(t)
	if host.labels["@0"].Name != "deckk" {
		t.Errorf("dashboard label %+v", host.labels["@0"])
	}
	if l := host.labels["@1"]; l.Name != "Inside deckk" || l.Status != "busy" {
		t.Errorf("session label %+v", l)
	}
}

func TestNewSessionPickerStartsClaudeInTheChosenFolder(t *testing.T) {
	m, host := newTestModel(t)
	target := filepath.Join(t.TempDir(), "fresh-app")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}

	m, _ = press(m, "n")
	if m.picker == nil {
		t.Fatal("n should open the folder picker")
	}
	for _, r := range target {
		m, _ = press(m, string(r))
	}
	if m.picker.shown[0] != target {
		t.Fatalf("typed path should be offered first, got %v", m.picker.shown)
	}
	m, cmd := press(m, "enter")
	run(cmd)
	if m.picker != nil || !slices.Equal(host.opened, []string{target + " claude"}) {
		t.Fatalf("opened %v", host.opened)
	}
}

func TestPickerCreatesAMissingFolder(t *testing.T) {
	m, host := newTestModel(t)
	target := filepath.Join(t.TempDir(), "brand-new")

	m, _ = press(m, "n")
	for _, r := range target {
		m, _ = press(m, string(r))
	}
	if m.picker.create != target {
		t.Fatalf("should offer to create %s, got %q", target, m.picker.create)
	}
	_, cmd := press(m, "enter")
	run(cmd)
	if !isDir(target) || !slices.Equal(host.opened, []string{target + " claude"}) {
		t.Fatalf("created %v, opened %v", isDir(target), host.opened)
	}
}

func TestRestoreReopensLastWorkspaceButNotPins(t *testing.T) {
	m, host := newTestModel(t)
	if err := m.store.SetOpen([]string{"bbbbbbbb"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := m.store.TogglePin("cccccccc"); err != nil {
		t.Fatal(err)
	}
	_, cmd := press(m, "R")
	run(cmd)
	if len(host.opened) != 1 {
		t.Fatalf("opened %v", host.opened)
	}
}

func TestWorkspaceForgetsOnlySessionsItSawClose(t *testing.T) {
	m, host := newTestModel(t)
	if !m.store.Open["dddddddd"] {
		t.Fatal("a session open in deckk should join the workspace")
	}
	if err := m.store.SetOpen([]string{"aaaaaaaa"}, nil); err != nil {
		t.Fatal(err)
	}

	host.windows = host.windows[:1:1]
	tm, _ := m.Update(loadedMsg{sessions: m.sessions, windows: host.windows})
	m = tm.(Model)
	if m.store.Open["dddddddd"] {
		t.Fatal("a session seen closing should leave the workspace")
	}
	if !m.store.Open["aaaaaaaa"] {
		t.Fatal("a session never seen in this dashboard should stay, as after a reboot")
	}
}

func TestTabNamesCannotInjectTmuxFormats(t *testing.T) {
	cases := map[string]string{
		"Fix #(rm -rf ~) now":                   "Fix ##(rm -rf ~) now",
		"refactor foo;":                         "refactor foo",
		"a very long title that goes on and on": "a very long title that …",
	}
	for in, want := range cases {
		if got := tabName(in); got != want {
			t.Errorf("tabName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResumeRefusesAMissingFolder(t *testing.T) {
	m, host := newTestModel(t)
	m = selectID(m, "aaaaaaaa")
	m.flat[m.cursor].Dir = "/nonexistent/deckk-test"
	m, cmd := press(m, "enter")
	for _, msg := range run(cmd) {
		if f, ok := msg.(flashMsg); ok {
			tm, _ := m.Update(f)
			m = tm.(Model)
		}
	}
	if len(host.opened) != 0 || !m.flashErr {
		t.Fatalf("opened %v, flash %q", host.opened, m.flash)
	}
}

func TestNewButtonOpensThePickerEvenWhileFiltering(t *testing.T) {
	m, _ := newTestModel(t)
	m, _ = press(m, "/")
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyF12})
	if tm.(Model).picker == nil {
		t.Fatal("F12 from the + new button should open the picker")
	}
}

func TestNewButtonLeavesSettings(t *testing.T) {
	m, _ := newTestModel(t)
	m, _ = press(m, "s")
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyF12})
	m = tm.(Model)
	if m.settings || m.picker == nil {
		t.Fatal("+ new should swap settings for the folder picker")
	}
}

func TestPopOutAndBringBackNeverRestartClaude(t *testing.T) {
	m, host := newTestModel(t)

	m = selectID(m, "dddddddd")
	_, cmd := press(m, "o")
	run(cmd)
	if !slices.Equal(host.popped, []string{"@1"}) {
		t.Fatalf("popped %v", host.popped)
	}

	m = selectID(m, "ffffffff")
	_, cmd = press(m, "m")
	run(cmd)
	if !slices.Equal(host.brought, []string{"@2"}) || len(host.released) != 0 {
		t.Fatalf("brought %v, released %v", host.brought, host.released)
	}
}

func TestEnterOnAPoppedOutSessionFocusesItsTabOrBringsItBack(t *testing.T) {
	m, host := newTestModel(t)
	m = selectID(m, "ffffffff")
	_, cmd := press(m, "enter")
	run(cmd)
	if len(host.brought) != 0 {
		t.Fatal("with its tab open, enter should only focus it")
	}

	host.windows[2].ClientTTY = ""
	tm, _ := m.Update(loadedMsg{sessions: m.sessions, windows: host.windows})
	m = selectID(tm.(Model), "ffffffff")
	_, cmd = press(m, "enter")
	run(cmd)
	if !slices.Equal(host.brought, []string{"@2"}) {
		t.Fatalf("with its tab closed, enter should bring it back, brought %v", host.brought)
	}
}

func TestRenameSetsTheTitleEverywhere(t *testing.T) {
	m, host := newTestModel(t)
	m = selectID(m, "dddddddd")
	m, _ = press(m, "r")
	if m.renaming == nil || m.renaming.Value() != "Inside deckk" {
		t.Fatal("r should open rename with the current title")
	}
	m.renaming.SetValue("Checkout flow")
	m, cmd := press(m, "enter")
	run(cmd)
	if s, _ := m.selected(); s.Title != "Checkout flow" {
		t.Fatalf("row title %q", s.Title)
	}
	if host.labels["@1"].Name != "Checkout flow" {
		t.Fatalf("tab name %q", host.labels["@1"].Name)
	}
}

func reload(m Model, sessions []claude.Session, windows []tmux.Window) Model {
	tm, cmd := m.Update(loadedMsg{sessions: sessions, windows: windows})
	run(cmd)
	return tm.(Model)
}

func stopped(sessions []claude.Session, id string) []claude.Session {
	out := slices.Clone(sessions)
	for i := range out {
		if out[i].ID == id {
			out[i].PID, out[i].TTY = 0, ""
		}
	}
	return out
}

func TestSessionsThatStopLandInRecentlyClosed(t *testing.T) {
	m, host := newTestModel(t)
	m = reload(m, stopped(m.sessions, "eeeeeeee"), host.windows)

	if _, ok := m.store.Closed["eeeeeeee"]; !ok {
		t.Fatal("a session that stopped should be recorded as closed")
	}
	i := slices.IndexFunc(m.groups, func(g deck.Group) bool { return g.Section == deck.Closed })
	if i < 0 || m.groups[i].Sessions[0].ID != "eeeeeeee" {
		t.Fatal("it should be listed under RECENTLY CLOSED")
	}
	if !slices.ContainsFunc(deck.ToRestore(m.sessions, m.marks()), func(s claude.Session) bool { return s.ID == "eeeeeeee" }) {
		t.Fatal("R should bring a recently closed session back")
	}
}

func TestClosingOnPurposeSkipsRecentlyClosed(t *testing.T) {
	m, host := newTestModel(t)
	m = selectID(m, "dddddddd")
	m, _ = press(m, "x")
	m, _ = press(m, "x")
	m = reload(m, stopped(m.sessions, "dddddddd"), host.windows[1:])
	if _, ok := m.store.Closed["dddddddd"]; ok {
		t.Fatal("a session closed with x should not count as recently closed")
	}
}

func TestFinishingWhileAwayMarksASessionNew(t *testing.T) {
	m, host := newTestModel(t)
	sessions := slices.Clone(m.sessions)
	for i := range sessions {
		if sessions[i].ID == "dddddddd" {
			sessions[i].Status, sessions[i].StatusAt = "idle", time.Now()
		}
	}
	m = reload(m, sessions, host.windows)
	if s := m.flat[indexOf(m, "dddddddd")]; !m.unread(s) {
		t.Fatal("a session that finished while you were elsewhere should be new")
	}
	if host.labels["@1"].Status != "fresh" {
		t.Fatalf("its tab should be highlighted, got %+v", host.labels["@1"])
	}

	host.windows[1].Active = true
	m = reload(m, sessions, host.windows)
	if s := m.flat[indexOf(m, "dddddddd")]; m.unread(s) {
		t.Fatal("looking at it should clear new")
	}
}

func TestBulkMovesInBothDirections(t *testing.T) {
	m, host := newTestModel(t)
	_, cmd := press(m, "O")
	run(cmd)
	if !slices.Equal(host.popped, []string{"@1"}) {
		t.Fatalf("O should pop out every strip session, popped %v", host.popped)
	}

	_, cmd = press(m, "M")
	run(cmd)
	if !slices.Equal(host.brought, []string{"@2"}) || !slices.Equal(host.released, []string{"/dev/ttys200"}) {
		t.Fatalf("M should bring back popped and outside sessions, brought %v released %v", host.brought, host.released)
	}
}

func TestSectionHeadersOfferBulkMoves(t *testing.T) {
	m, host := newTestModel(t)
	_, owners := m.body()
	y := slices.Index(owners, moveInOwner) + headerLines - m.offset
	tm, cmd := m.Update(tea.MouseMsg{X: 4, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	_ = tm
	run(cmd)
	if len(host.brought) != 1 {
		t.Fatalf("clicking the header should move everything in, brought %v", host.brought)
	}
}

func TestSessionsLostInARestartCountAsRecentlyClosed(t *testing.T) {
	m, _ := newTestModelWith(t, func(st *store.Store) {
		if err := st.SetRunning(map[string]bool{"aaaaaaaa": true, "dddddddd": true}); err != nil {
			t.Fatal(err)
		}
	})
	if _, ok := m.store.Closed["aaaaaaaa"]; !ok {
		t.Fatal("a session running before the restart and gone now should be recently closed")
	}
	if _, ok := m.store.Closed["dddddddd"]; ok {
		t.Fatal("a session still running is not closed")
	}
	if !m.store.Running["eeeeeeee"] || m.store.Running["aaaaaaaa"] {
		t.Fatalf("running set should be refreshed, got %v", m.store.Running)
	}
}

func TestTabsShrinkThenTurnIntoNumbers(t *testing.T) {
	if fit, compact := tabFit(200, 2); fit != maxTabName || compact {
		t.Errorf("two tabs on a wide screen keep full names, got %d %v", fit, compact)
	}
	fit, compact := tabFit(120, 6)
	if compact || fit >= maxTabName || fit < 6 {
		t.Errorf("six tabs should shrink names, got %d %v", fit, compact)
	}
	if _, compact := tabFit(80, 12); !compact {
		t.Error("twelve tabs on 80 columns should switch to numbers")
	}
}

func TestNumberKeysJumpToStripTabs(t *testing.T) {
	m, host := newTestModel(t)
	_, cmd := press(m, "1")
	run(cmd)
	if !slices.Equal(host.selected, []string{"@1"}) {
		t.Fatalf("selected %v", host.selected)
	}
}

func TestSettingsShapeTheClaudeCommand(t *testing.T) {
	m, host := newTestModel(t)
	m, _ = press(m, "s")
	if !m.settings {
		t.Fatal("s should open settings")
	}
	for range 3 {
		m, _ = press(m, "enter")
	}
	m, _ = press(m, "esc")
	if m.cfg.View != "focus" {
		t.Fatalf("view %q", m.cfg.View)
	}

	m = selectID(m, "aaaaaaaa")
	_, cmd := press(m, "enter")
	run(cmd)
	if len(host.opened) != 1 || !strings.Contains(host.opened[0], `--settings '{"viewMode":"focus"}'`) {
		t.Fatalf("opened %v", host.opened)
	}
}

func TestLongListsCollapseUntilAskedOrFiltered(t *testing.T) {
	m, _ := newTestModel(t)
	now := time.Now()
	for i := range 25 {
		m.sessions = append(m.sessions, claude.Session{ID: fmt.Sprintf("old%02d", i), Title: fmt.Sprintf("Old %d", i), Cwd: "/x", UpdatedAt: now.Add(-time.Duration(i+3) * time.Hour)})
	}
	m.rearrange()
	recent := m.groups[len(m.groups)-1]
	if len(recent.Sessions) != 10 || recent.Hidden != 18 {
		t.Fatalf("recent shows %d, hides %d", len(recent.Sessions), recent.Hidden)
	}

	m, _ = press(m, "+")
	if g := m.groups[len(m.groups)-1]; g.Hidden != 0 || len(g.Sessions) != 28 {
		t.Fatalf("+ should show everything, got %d hidden", g.Hidden)
	}
	m, _ = press(m, "+")

	m, _ = press(m, "/")
	for _, r := range "Old 2" {
		m, _ = press(m, string(r))
	}
	if len(m.groups) != 1 || len(m.groups[0].Sessions) != 7 || m.groups[0].Hidden != 0 {
		t.Fatalf("find should search every session, got %+v", m.groups)
	}
}

func TestBackgroundSessionsGetTheirOwnView(t *testing.T) {
	m, _ := newTestModel(t)
	m.sessions = append(m.sessions,
		claude.Session{ID: "bg1", Title: "Analyze the PR data from stdin", Cwd: "/x", Background: true, UpdatedAt: time.Now()},
		claude.Session{ID: "bg2", Title: "Summarize the diff", Cwd: "/x", Background: true, UpdatedAt: time.Now(), PID: 9, Status: "busy"},
	)
	m.rearrange()
	for _, s := range m.flat {
		if s.Background {
			t.Fatalf("%s should not be on the Sessions view", s.ID)
		}
	}

	m, _ = press(m, "tab")
	if m.view != boardView {
		t.Fatalf("tab should reach Board first, got %v", m.view)
	}
	m, _ = press(m, "tab")
	if m.view != backgroundView || len(m.flat) != 2 {
		t.Fatalf("view %v shows %d", m.view, len(m.flat))
	}

	m, _ = press(m, "tab")
	if m.view != findView {
		t.Fatalf("tab should reach Find, got %v", m.view)
	}
	for _, r := range "pr data" {
		m, _ = press(m, string(r))
	}
	if len(m.flat) != 1 || m.flat[0].ID != "bg1" {
		t.Fatalf("find should include background sessions, got %d", len(m.flat))
	}
	m, _ = press(m, "esc")
	m, _ = press(m, "esc")
	if m.view != sessionsView {
		t.Fatalf("esc twice should clear then leave Find, got %v", m.view)
	}
}

func TestClickingAViewTabSwitchesToIt(t *testing.T) {
	m, _ := newTestModel(t)
	x := -1
	for i := range m.width {
		if v, ok := m.viewTabAt(i, 0); ok && v == findView {
			x = i
			break
		}
	}
	if x < 0 {
		t.Fatal("Find tab not found")
	}
	m, _ = click(m, x, 0)
	if m.view != findView {
		t.Fatalf("view %v", m.view)
	}
}

func TestBackgroundSessionsNeverCountAsClosed(t *testing.T) {
	m, host := newTestModel(t)
	sessions := append(slices.Clone(m.sessions), claude.Session{ID: "bg", Background: true, PID: 7, TTY: "", Status: "busy", Cwd: "/x"})
	m = reload(m, sessions, host.windows)
	m = reload(m, sessions[:len(sessions)-1], host.windows)
	if _, ok := m.store.Closed["bg"]; ok {
		t.Fatal("a headless run finishing should not land in RECENTLY CLOSED")
	}
}

func TestModelName(t *testing.T) {
	for id, want := range map[string]string{"claude-opus-5-5": "opus 5.5", "claude-sonnet-4-5-20250929": "sonnet 4.5", "claude-fable-5-2": "fable 5.2"} {
		if got := modelName(id); got != want {
			t.Errorf("%s: got %q, want %q", id, got, want)
		}
	}
}

func TestChangingTabKeysRebindsThem(t *testing.T) {
	m, host := newTestModel(t)
	m.settings = true
	m.setting = slices.IndexFunc(config.Options, func(o config.Option) bool { return o.Key == "tab_keys" })
	_, cmd := press(m, "enter")
	run(cmd)
	if !slices.Equal(host.tabKeys, []string{config.TabKeysBrackets}) {
		t.Fatalf("set %v", host.tabKeys)
	}
}

func TestPinButtonPinsAndShrinksTheTabYouAreOn(t *testing.T) {
	m, host := newTestModel(t)
	host.windows[1].Active = true
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF11})
	for _, msg := range run(cmd) {
		tm, cmd = tm.Update(msg)
		for _, msg := range run(cmd) {
			if labels, ok := msg.(labelsSetMsg); ok {
				tm, _ = tm.Update(labels)
			}
		}
	}
	m = tm.(Model)
	if !m.store.Pinned["dddddddd"] {
		t.Fatal("the pin button should pin the session in the active tab")
	}
	if !slices.Equal(host.moved, []string{"@1>@0"}) {
		t.Fatalf("moved %v, want the pinned tab right after the dashboard", host.moved)
	}
	if got := host.labels["@1"]; got.Name != "Inside" || !strings.Contains(got.Info, " unpin ") {
		t.Fatalf("label %+v", got)
	}
}

func TestBandDropsDetailsToFit(t *testing.T) {
	s := claude.Session{Status: "busy", Cwd: "/tmp/x", Model: "claude-opus-5-5", Context: 120000, StartedAt: time.Now().Add(-time.Hour)}
	wide, narrow := tabInfo(s, time.Now(), bandInfo{width: 200}), tabInfo(s, time.Now(), bandInfo{width: 50})
	if !strings.Contains(wide, "up ") || strings.Contains(narrow, "up ") || !strings.Contains(narrow, " pin ") {
		t.Fatalf("wide %q\nnarrow %q", wide, narrow)
	}
	if bandWidth(narrow) > 50 {
		t.Fatalf("narrow band is %d wide", bandWidth(narrow))
	}
}
