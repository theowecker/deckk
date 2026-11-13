package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/deck"
)

var (
	clay  = lipgloss.AdaptiveColor{Light: "#C2562F", Dark: "#E8875B"}
	blue  = lipgloss.AdaptiveColor{Light: "#2F5BB7", Dark: "#7AA2F7"}
	red   = lipgloss.AdaptiveColor{Light: "#B42318", Dark: "#F97066"}
	fg    = lipgloss.AdaptiveColor{Light: "#1F1F1F", Dark: "#ECECEC"}
	muted = lipgloss.AdaptiveColor{Light: "#6B6B6B", Dark: "#8A8A8A"}
	faint = lipgloss.AdaptiveColor{Light: "#A3A3A3", Dark: "#565656"}

	accent  = lipgloss.NewStyle().Foreground(clay)
	brand   = lipgloss.NewStyle().Foreground(clay).Bold(true)
	text    = lipgloss.NewStyle().Foreground(fg)
	bold    = lipgloss.NewStyle().Foreground(fg).Bold(true)
	dim     = lipgloss.NewStyle().Foreground(muted)
	ghost   = lipgloss.NewStyle().Foreground(faint)
	working = lipgloss.NewStyle().Foreground(blue)
	errText = lipgloss.NewStyle().Foreground(red)
	preview = lipgloss.NewStyle().Foreground(muted).Italic(true)
	reply   = lipgloss.NewStyle().Foreground(muted)
	soft    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#3A3A3A", Dark: "#C4C4CC"})
	heading = lipgloss.NewStyle().Foreground(muted).Bold(true)
	rule    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#D6D6D6", Dark: "#3A3A52"})
)

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const splashFor = 900 * time.Millisecond

const (
	gutter      = 2
	statusWidth = 13
	headerLines = 4
	footerLines = 2
)

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	if !m.loaded || time.Since(m.startedAt) < splashFor {
		return m.splash()
	}
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n\n")

	body, _ := m.body()
	height := m.bodyHeight()
	end := min(m.offset+height, len(body))
	visible := slices.Clone(body[min(m.offset, end):end])
	for i, line := range visible {
		visible[i] = ansi.Truncate(line, m.width-1, "…")
	}
	b.WriteString(strings.Join(visible, "\n"))
	b.WriteString(strings.Repeat("\n", height-len(visible)))

	b.WriteString("\n\n")
	b.WriteString(m.footer())
	return b.String()
}

func (m Model) header() string {
	busy := false
	for _, s := range m.sessions {
		busy = busy || deck.StateOf(s) == deck.Working
	}
	left := m.headerLeft()
	deckLines := miniDeck(m.frame, busy)
	deckWidth := lipgloss.Width(deckLines[0])
	if m.width < lipgloss.Width(left)+deckWidth+gutter*2 {
		return m.viewTabsLine() + "\n" + left + "\n"
	}
	lines := make([]string, 3)
	for i, d := range deckLines {
		l := ""
		switch i {
		case 0:
			l = m.viewTabsLine()
		case 1:
			l = left
		}
		gap := m.width - lipgloss.Width(l) - deckWidth - gutter
		lines[i] = l + strings.Repeat(" ", max(gap, 1)) + d
	}
	return strings.Join(lines, "\n")
}

func (m Model) headerLeft() string {
	if m.picker != nil {
		return pad() + brand.Render("New session") + "  " + m.picker.input.View()
	}
	if m.renaming != nil {
		return pad() + brand.Render("Rename") + "  " + m.renaming.View()
	}
	if m.settings {
		return pad() + brand.Render("Settings")
	}
	if m.view == findView {
		return pad() + m.filter.View()
	}

	var yourTurn, fresh, busy, resumable int
	for _, s := range m.inView(m.sessions) {
		if m.unread(s) {
			fresh++
		}
		switch deck.StateOf(s) {
		case deck.Working:
			busy++
		case deck.YourTurn:
			yourTurn++
		case deck.Stopped:
			if !m.store.Archived[s.ID] {
				resumable++
			}
		}
	}
	var parts []string
	add := func(n int, glyph, label string) {
		if n > 0 {
			parts = append(parts, glyph+" "+text.Render(fmt.Sprint(n))+" "+dim.Render(label))
		}
	}
	add(yourTurn, accent.Render("●"), "waiting")
	if fresh > 0 {
		parts[len(parts)-1] += dim.Render(" · ") + accent.Bold(true).Render(fmt.Sprintf("%d new", fresh))
	}
	add(busy, working.Render("◐"), "working")
	add(resumable, ghost.Render("○"), "resumable")
	return pad() + strings.Join(parts, "     ")
}

type hint struct{ key, label string }

func (m Model) hints() []hint {
	switch {
	case m.help:
		return []hint{{"esc", "close help"}}
	case m.picker != nil:
		return []hint{{"enter", "start claude"}, {"esc", "cancel"}}
	case m.renaming != nil:
		return []hint{{"enter", "save"}, {"esc", "cancel"}}
	case m.settings:
		return []hint{{"enter", "change"}, {"esc", "done"}}
	case m.view == findView:
		return []hint{{"enter", "open"}, {"↑↓", "move"}, {"esc", "back"}, {"tab", "next view"}}
	}
	var hints []hint
	if s, ok := m.selected(); ok {
		_, hosted := m.window(s)
		_, popped := m.poppedOut(s)
		switch {
		case hosted:
			hints = []hint{{"enter", "switch"}, {"o", "own tab"}, {"x", "close"}}
		case popped:
			hints = []hint{{"enter", "jump to tab"}, {"m", "bring back"}, {"x", "close"}}
		case s.Running():
			hints = []hint{{"enter", "jump to tab"}, {"m", "move into deckk"}}
		default:
			hints = []hint{{"enter", "resume"}}
		}
		pin := "pin"
		if m.store.Pinned[s.ID] {
			pin = "unpin"
		}
		hints = append(hints, hint{"p", pin}, hint{"r", "rename"})
		if _, ok := m.suggestion(s); ok {
			hints = append([]hint{hints[0], {"d", "dismiss suggestion"}}, hints[1:]...)
		}
	}
	view := "by project"
	if m.byProject {
		view = "by status"
	}
	// Least important last, since a narrow window cuts the end off.
	return append(hints, hint{"n", "new"}, hint{"c", "ask deckk"}, hint{"?", "all keys"}, hint{"q", "quit"}, hint{"s", "settings"}, hint{"/", "find"}, hint{"v", view})
}

const hintSeparator = "   "

func (m Model) footer() string {
	if m.flash != "" {
		style := accent
		if m.flashErr {
			style = errText
		}
		return pad() + style.Render(ansi.Truncate(m.flash, m.width-gutter*2, "…"))
	}
	var parts []string
	for _, h := range m.visibleHints() {
		parts = append(parts, text.Render(keyLabel(h.key))+" "+ghost.Render(h.label))
	}
	return pad() + strings.Join(parts, ghost.Render(hintSeparator))
}

func (m Model) visibleHints() []hint {
	var out []hint
	used := gutter
	for _, h := range m.hints() {
		w := lipgloss.Width(keyLabel(h.key) + " " + h.label)
		if len(out) > 0 {
			w += lipgloss.Width(hintSeparator)
		}
		if used+w > m.width-gutter {
			break
		}
		out = append(out, h)
		used += w
	}
	return out
}

// Must mirror footer's layout.
func (m Model) hintAt(x int) (string, bool) {
	if m.flash != "" {
		return "", false
	}
	start := gutter
	for _, h := range m.visibleHints() {
		end := start + lipgloss.Width(keyLabel(h.key)+" "+h.label)
		if x >= start && x < end {
			return h.key, true
		}
		start = end + lipgloss.Width(hintSeparator)
	}
	return "", false
}

func keyLabel(key string) string {
	if key == "enter" {
		return "↵"
	}
	return key
}

func (m Model) footerY() int {
	return headerLines + m.bodyHeight() + 1
}

func (m Model) bodyHeight() int {
	return max(m.height-headerLines-footerLines-1, 1)
}

const (
	noOwner      = -1
	restoreOwner = -2
	popAllOwner  = -3
	moveInOwner  = -4
	moreOwner    = -5
)

// body also returns what each line belongs to, so scrolling, clicks and
// rendering can never disagree about layout.
func (m Model) body() ([]string, []int) {
	if m.help {
		return helpBody(m.frame)
	}
	if m.settings {
		return m.settingsBody()
	}
	if m.picker != nil {
		return m.pickerBody()
	}
	if !m.loaded {
		return []string{pad() + dim.Render("Reading sessions…")}, []int{-1}
	}
	if len(m.flat) == 0 {
		msg := "No Claude sessions yet. Press n to start one in a folder."
		switch m.view {
		case backgroundView:
			msg = "No background sessions. Headless runs from claude -p, plugins and scripts show up here."
		case findView:
			msg = "No session matches that."
		}
		return []string{pad() + dim.Render(msg)}, []int{noOwner}
	}

	var lines []string
	var owners []int
	add := func(line string, owner int) {
		lines = append(lines, line)
		owners = append(owners, owner)
	}
	if m.view == sessionsView {
		if n := len(deck.ToRestore(m.sessions, m.marks())); n > 0 {
			add(pad()+accent.Render("↻ ")+text.Render(plural(n, "session")+" to bring back")+
				"   "+text.Render("R")+" "+ghost.Render("reopen all"), restoreOwner)
			add("", noOwner)
		}
		if !m.byProject && (len(m.groups) == 0 || m.groups[0].Section != deck.Running) {
			add(pad()+dim.Render("Nothing is open in deckk yet. Press n to start Claude in a folder, or ↵ on any session below to bring it here."), noOwner)
			add("", noOwner)
		}
	}

	index := 0
	now := time.Now()
	titles, projects := m.columns()
	for gi, g := range m.groups {
		if gi > 0 {
			add("", -1)
		}
		switch {
		case m.byProject:
			add(m.sectionTitle(g, "", ""), noOwner)
		case g.Section == deck.Running:
			add(m.sectionTitle(g, "O", "give each its own tab"), popAllOwner)
		case g.Section == deck.Elsewhere:
			add(m.sectionTitle(g, "M", "move all into deckk"), moveInOwner)
		default:
			add(m.sectionTitle(g, "", ""), noOwner)
		}
		for _, s := range g.Sessions {
			selected := index == m.cursor
			if g.Section == deck.NextUp {
				add(m.suggestionRow(s, selected, titles), index)
				if selected {
					for _, line := range m.preview(s) {
						add(line, index)
					}
				}
				index++
				continue
			}
			add(m.row(s, selected, now, titles, projects), index)
			if selected {
				for _, line := range m.preview(s) {
					add(line, index)
				}
			}
			index++
		}
		if g.Hidden > 0 {
			add(pad()+"  "+dim.Render(fmt.Sprintf("+ %d more", g.Hidden))+"   "+text.Render("+")+" "+ghost.Render("show all"), moreOwner)
		}
	}
	if m.showAll && m.view != findView {
		add("", noOwner)
		add(pad()+"  "+text.Render("+")+" "+ghost.Render("show fewer"), moreOwner)
	}
	return lines, owners
}

// sectionTitle is a heading with its count, a rule out to the status column
// and, when the section has one, its action key at the end.
func (m Model) sectionTitle(g deck.Group, key, label string) string {
	head := heading.Render(g.Title) + "  " + ghost.Render(fmt.Sprint(len(g.Sessions)+g.Hidden))
	end := ""
	if key != "" {
		end = " " + text.Render(key) + " " + ghost.Render(label)
	}
	width := lipgloss.Width(head) + lipgloss.Width(end)
	line := rule.Render(strings.Repeat("─", max(m.width-gutter*2-width-1, 2)))
	return pad() + head + " " + line + end
}

func (m Model) rowRoom() int {
	return max(m.width-gutter-2-2-2-statusWidth, 10)
}

// columns are how wide titles and folders are padded, so folders and
// branches line up down the list. Titles never take more than three fifths
// of the row, folders no more than 18 cells.
func (m Model) columns() (int, int) {
	titles, projects := 0, 0
	for _, g := range m.groups {
		for _, s := range g.Sessions {
			titles = max(titles, lipgloss.Width(rowTitle(s)))
			projects = max(projects, lipgloss.Width(s.Project()))
		}
	}
	return min(titles, m.rowRoom()*3/5), min(projects, 18)
}

func rowTitle(s claude.Session) string {
	if s.Title == "" {
		return "new session"
	}
	return displayTitle(s)
}

func (m Model) row(s claude.Session, selected bool, now time.Time, titles, projects int) string {
	bar := "  "
	titleStyle := soft
	if s.Running() {
		titleStyle = text
	}
	if selected {
		bar = accent.Render("▌ ")
		titleStyle = bold
	}
	if m.unread(s) {
		titleStyle = titleStyle.Foreground(clay)
	}

	glyph, status := m.status(s, now)
	room := m.rowRoom()
	titles = min(titles, room)
	title := ansi.Truncate(rowTitle(s), titles, "…")
	content := titleStyle.Render(title) + strings.Repeat(" ", titles-lipgloss.Width(title))
	used := titles

	var meta []string
	project := ansi.Truncate(s.Project(), projects, "…")
	meta = append(meta, dim.Render(project)+strings.Repeat(" ", projects-lipgloss.Width(project)))
	if s.Branch != "" && s.Branch != "HEAD" {
		meta = append(meta, ghost.Render("⎇ ")+dim.Render(s.Branch))
	}
	if _, popped := m.poppedOut(s); popped {
		meta = append(meta, ghost.Render("own tab"))
	}
	if s.Running() && m.store.Pinned[s.ID] {
		meta = append(meta, ghost.Render("◆"))
	}
	if _, ok := m.suggestion(s); ok {
		meta = append(meta, accent.Render("✦"))
	}
	if metaRoom := room - used - 3; metaRoom > 3 {
		info := strings.TrimRight(ansi.Truncate(strings.Join(meta, "  "), metaRoom, "…"), " ")
		content += "   " + info
		used += 3 + lipgloss.Width(info)
	}
	fill := strings.Repeat(" ", max(room-used, 0))
	return bar + glyph + " " + content + fill + "  " + lipgloss.PlaceHorizontal(statusWidth, lipgloss.Right, status)
}

// suggestionRow is a Next up row: the session and the prompt deckk
// suggests for it, which is already typed in once it says ready.
func (m Model) suggestionRow(s claude.Session, selected bool, titles int) string {
	sg, _ := m.suggestion(s)
	bar, titleStyle := "  ", text
	if selected {
		bar, titleStyle = accent.Render("▌ "), bold
	}
	room := m.rowRoom()
	titles = min(titles, room)
	title := ansi.Truncate(rowTitle(s), titles, "…")
	line := titleStyle.Render(title) + strings.Repeat(" ", titles-lipgloss.Width(title))
	if promptRoom := room - titles - 3; promptRoom > 3 {
		prompt := ansi.Truncate("› "+sg.Prompt, promptRoom, "…")
		line += "   " + accent.Render(prompt) + strings.Repeat(" ", promptRoom-lipgloss.Width(prompt))
	}
	state := ghost.Render("suggested")
	if m.prefilled(s, sg) {
		state = accent.Render("ready")
	}
	return bar + accent.Render("✦") + " " + line + "  " + lipgloss.PlaceHorizontal(statusWidth, lipgloss.Right, state)
}

func (m Model) status(s claude.Session, now time.Time) (string, string) {
	since := ""
	if !s.StatusAt.IsZero() && now.Sub(s.StatusAt) >= time.Minute {
		since = " " + ghost.Render(deck.For(s.StatusAt, now))
	}
	switch deck.StateOf(s) {
	case deck.Working:
		return working.Render(spinner[m.frame%len(spinner)]), working.Render("working") + since
	case deck.YourTurn:
		if m.unread(s) {
			return accent.Render("●"), accent.Render("waiting ") + accent.Bold(true).Render("new")
		}
		return accent.Render("●"), accent.Render("waiting") + since
	case deck.Other:
		return ghost.Render("●"), dim.Render(s.Status)
	}
	if at, ok := m.store.Closed[s.ID]; ok && now.Sub(at) < deck.ClosedFor {
		return ghost.Render("○"), dim.Render("closed ") + ghost.Render(deck.For(at, now))
	}
	switch {
	case m.store.Pinned[s.ID]:
		return accent.Render("◆"), ghost.Render(deck.Ago(s.UpdatedAt, now))
	default:
		return ghost.Render("○"), ghost.Render(deck.Ago(s.UpdatedAt, now))
	}
}

func (m Model) preview(s claude.Session) []string {
	const indent = "    "
	room := m.width - gutter*2 - len(indent)
	var lines []string
	if sg, ok := m.suggestion(s); ok && sg.Why != "" {
		lines = append(lines, pad()+indent+accent.Render(ansi.Truncate("✦ "+sg.Why, room, "…")))
	}
	if s.LastPrompt != "" {
		lines = append(lines, pad()+indent+preview.Render(ansi.Truncate("› "+s.LastPrompt, room, "…")))
	}
	if s.LastReply != "" {
		lines = append(lines, pad()+indent+reply.Render(ansi.Truncate("⏺ "+s.LastReply, room, "…")))
	}
	if s.PR != "" {
		lines = append(lines, pad()+indent+accent.Render("⎇ PR "+s.PR)+"  "+ghost.Render(ansi.Truncate(s.PRURL, room-12, "…")))
	}
	return lines
}

func (m *Model) scroll() {
	_, owners := m.body()
	cursor := m.activeCursor()
	first := max(slices.Index(owners, cursor), 0)
	last := first
	for i := first; i < len(owners) && owners[i] == cursor; i++ {
		last = i
	}
	height := m.bodyHeight()
	switch {
	case first < m.offset:
		m.offset = max(first-1, 0)
	case last >= m.offset+height:
		m.offset = min(last+2-height, first)
	}
}

func (m Model) activeCursor() int {
	if m.settings {
		return m.setting
	}
	if m.picker != nil {
		return m.picker.cursor
	}
	return m.cursor
}

func (m Model) pickerBody() ([]string, []int) {
	p := m.picker
	if len(p.shown) == 0 {
		return []string{pad() + dim.Render("No folder matches. Type a full path like ~/dev/new-app")}, []int{-1}
	}
	lines := []string{pad() + ghost.Render(fmt.Sprintf("FOLDERS  %d", len(p.shown)))}
	owners := []int{-1}
	for i, dir := range p.shown {
		bar, style := "  ", text
		if i == p.cursor {
			bar, style = accent.Render("▌ "), bold
		}
		name := style.Render(filepath.Base(dir))
		if dir == p.create {
			name = accent.Render("+ create ") + name
		}
		line := name + "  " + dim.Render(abbreviateHome(filepath.Dir(dir)))
		lines = append(lines, bar+ansi.Truncate(line, m.width-gutter*2, "…"))
		owners = append(owners, i)
	}
	return lines, owners
}

func helpBody(frame int) ([]string, []int) {
	sections := []struct {
		title string
		keys  [][2]string
	}{
		{"ON THE DASHBOARD", [][2]string{
			{"↵  or double-click", "switch to a session, jump to its tab, or resume it here"},
			{"n", "start Claude in a folder, or type a new path to create one"},
			{"c", "talk to the deckk session beside the list, or start it"},
			{"⌃⇧↑ ⌃⇧↓", "move between the list and the deckk session"},
			{"d", "dismiss deckk's suggestion for a session"},
			{"x", "close a session running in deckk. It stays resumable"},
			{"o   O", "give this session, or every one, its own tab"},
			{"m   M", "bring this session, or all of them, from other tabs into deckk"},
			{"R", "reopen everything: last workspace and anything closed today"},
			{"p", "pin a session to keep it in the Pinned list and shrink its tab"},
			{"r", "rename a session. Leave it empty to go back to Claude's title"},
			{"a", "archive a session. Find still lists it"},
			{"tab", "switch between Sessions, Background and Find"},
			{"+", "show every session, or only the latest few"},
			{"/   f", "find any session, ever, by title, project, prompt or reply"},
			{"v", "group sessions by status or by project"},
			{"s", "settings for the Claude sessions deckk starts"},
			{"q", "quit. Sessions keep running and deckk brings you back"},
		}},
		{"ANYWHERE IN DECKK", [][2]string{
			{"⌃Space", "flip between the dashboard and your last session"},
			{"⌃⇧←  ⌃⇧→", "previous or next tab. ⌘⇧← and ⌘⇧→ in iTerm2"},
			{"1 to 9", "on the dashboard, jump to that tab in the strip"},
			{"click a tab", "switch to that session"},
			{"click ▴", "hide the info line under the tabs, ▾ brings it back"},
			{"drag a tab", "drop it on another tab to move it there"},
			{"click pin", "pin the tab you're on, from the line under the tabs"},
			{"scroll", "scroll back through a session"},
		}},
		{"FROM A SHELL", [][2]string{
			{"deckk", "open deckk, or come back to it"},
			{"deckk stop", "end deckk and every session running in it"},
		}},
	}
	lines := []string{}
	for _, line := range strings.Split(cassette(frame), "\n") {
		lines = append(lines, pad()+line)
	}
	lines = append(lines, "")
	for i, sec := range sections {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, pad()+ghost.Render(sec.title))
		for _, k := range sec.keys {
			lines = append(lines, pad()+text.Render(fmt.Sprintf("%-20s", k[0]))+dim.Render(k[1]))
		}
	}
	owners := make([]int, len(lines))
	for i := range owners {
		owners[i] = noOwner
	}
	return lines, owners
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func displayTitle(s claude.Session) string {
	if s.Title != "" {
		return s.Title
	}
	if s.Cwd != "" {
		return s.Project()
	}
	return "untitled " + claude.ShortID(s.ID)
}

func pad() string { return strings.Repeat(" ", gutter) }
