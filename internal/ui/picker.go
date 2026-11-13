package ui

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/deck"
)

type picker struct {
	input    textinput.Model
	all      []string
	shown    []string
	create   string
	cursor   int
	pool     []claude.Session
	sessions []claude.Session
	// project is the folder whose sessions the picker lists, or empty while
	// it lists every folder.
	project string
}

const pickerSessions = 12

func (m Model) openPicker() (tea.Model, tea.Cmd) {
	ti := textinput.New()
	ti.Prompt = "in "
	ti.Placeholder = "a folder, an old session, or a new path like ~/dev/app"
	ti.PromptStyle = dim
	ti.Cursor.Style = accent
	p := &picker{input: ti, all: candidateDirs(m.sessions), pool: m.pickerPool()}
	p.refilter()
	m.picker = p
	m.offset = 0
	cmds := []tea.Cmd{p.input.Focus()}
	if m.everything == nil {
		cmds = append(cmds, m.findEverything())
	}
	return m, tea.Batch(cmds...)
}

func (m Model) pickerPool() []claude.Session {
	pool := m.everything
	if pool == nil {
		pool = m.sessions
	}
	return slices.DeleteFunc(slices.Clone(pool), func(s claude.Session) bool { return s.Background })
}

func (m Model) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.picker
	switch msg.String() {
	case "esc", "ctrl+c":
		if p.project != "" && msg.String() == "esc" {
			p.leaveProject()
			return m, nil
		}
		m.picker = nil
		m.scroll()
		return m, nil
	case "enter":
		if p.cursor < len(p.shown) {
			dir := p.shown[p.cursor]
			m.picker = nil
			return m, m.newSession(dir, dir == p.create)
		}
		if i := p.cursor - len(p.shown); i < len(p.sessions) {
			s := p.sessions[i]
			m.picker = nil
			return m, m.openWithSuggestion(s)
		}
		return m, nil
	case "right", "tab":
		if p.project == "" && p.cursor < len(p.shown) && p.shown[p.cursor] != p.create {
			p.enterProject(p.shown[p.cursor])
			m.offset = 0
		}
		return m, nil
	case "left":
		if p.project != "" && p.input.Value() == "" {
			p.leaveProject()
			return m, nil
		}
	case "up", "ctrl+p":
		m.movePicker(-1)
		return m, nil
	case "down", "ctrl+n":
		m.movePicker(1)
		return m, nil
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.refilter()
	m.offset = 0
	return m, cmd
}

func (p *picker) enterProject(dir string) {
	p.project = dir
	p.input.SetValue("")
	p.refilter()
	p.cursor = 0
}

func (p *picker) leaveProject() {
	dir := p.project
	p.project = ""
	p.input.SetValue("")
	p.refilter()
	p.cursor = max(0, slices.Index(p.shown, dir))
}

func (p *picker) rows() int { return len(p.shown) + len(p.sessions) }

func (m *Model) movePicker(delta int) {
	p := m.picker
	p.cursor = max(0, min(p.cursor+delta, p.rows()-1))
	m.scroll()
}

func (p *picker) refilter() {
	query := strings.TrimSpace(p.input.Value())
	p.shown = p.shown[:0]
	p.create = ""
	p.sessions = p.sessions[:0]
	p.cursor = 0
	if p.project != "" {
		p.shown = append(p.shown, p.project)
		for _, s := range p.pool {
			if inFolder(s, p.project) && deck.Matches(s, query) {
				p.sessions = append(p.sessions, s)
			}
		}
		sortNewest(p.sessions)
		return
	}
	if strings.HasPrefix(query, "/") || strings.HasPrefix(query, "~") {
		dir := filepath.Clean(expandHome(query))
		switch {
		case isDir(dir):
			p.shown = append(p.shown, dir)
		case isDir(filepath.Dir(dir)):
			p.create = dir
			p.shown = append(p.shown, dir)
		}
	}
	for _, dir := range p.all {
		if matchesPath(dir, query) && !slices.Contains(p.shown, dir) {
			p.shown = append(p.shown, dir)
		}
	}
	if query == "" {
		return
	}
	for _, s := range p.pool {
		if deck.Matches(s, query) {
			p.sessions = append(p.sessions, s)
		}
	}
	sortNewest(p.sessions)
	p.sessions = p.sessions[:min(len(p.sessions), pickerSessions)]
}

func inFolder(s claude.Session, dir string) bool {
	return filepath.Clean(s.Cwd) == dir || filepath.Clean(s.Dir) == dir
}

func sortNewest(sessions []claude.Session) {
	slices.SortFunc(sessions, func(a, b claude.Session) int {
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), strings.Compare(a.ID, b.ID))
	})
}

func matchesPath(dir, query string) bool {
	haystack := strings.ToLower(dir)
	for word := range strings.FieldsSeq(strings.ToLower(query)) {
		if !strings.Contains(haystack, word) {
			return false
		}
	}
	return true
}

// Siblings surface projects with no session yet. The home directory's
// siblings are never projects.
func candidateDirs(sessions []claude.Session) []string {
	sorted := slices.Clone(sessions)
	slices.SortFunc(sorted, func(a, b claude.Session) int { return b.UpdatedAt.Compare(a.UpdatedAt) })

	var dirs []string
	parents := map[string]bool{}
	for _, s := range sorted {
		if s.Cwd == "" || slices.Contains(dirs, s.Cwd) || !isDir(s.Cwd) {
			continue
		}
		dirs = append(dirs, s.Cwd)
		parents[filepath.Dir(s.Cwd)] = true
	}

	home, _ := os.UserHomeDir()
	delete(parents, home)
	delete(parents, "/")

	var siblings []string
	for parent := range parents {
		entries, err := os.ReadDir(parent)
		if err != nil {
			continue
		}
		for _, e := range entries {
			path := filepath.Join(parent, e.Name())
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && !slices.Contains(dirs, path) {
				siblings = append(siblings, path)
			}
		}
	}
	slices.Sort(siblings)
	return append(dirs, slices.Compact(siblings)...)
}

func expandHome(path string) string {
	if rest, ok := strings.CutPrefix(path, "~"); ok {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, rest)
	}
	return path
}

func abbreviateHome(path string) string {
	home, _ := os.UserHomeDir()
	if rest, ok := strings.CutPrefix(path, home); ok && home != "" {
		return "~" + rest
	}
	return path
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
