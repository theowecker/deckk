package ui

import (
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/deck"
	"github.com/theowecker/deckk/internal/hq"
)

// A proposed session shows in Next up as a session that doesn't exist yet.
// Its ID carries this prefix so nothing mistakes it for a real one.
const proposalPrefix = "proposal:"

func isProposal(s claude.Session) bool { return strings.HasPrefix(s.ID, proposalPrefix) }

func proposalSession(p hq.Proposal) claude.Session {
	return claude.Session{
		ID: proposalPrefix + p.ID, Cwd: p.Folder, Dir: p.Folder, UpdatedAt: p.At,
		Title: "New session in " + filepath.Base(p.Folder),
	}
}

func (m Model) proposal(s claude.Session) (hq.Proposal, bool) {
	id := strings.TrimPrefix(s.ID, proposalPrefix)
	i := slices.IndexFunc(m.deckk.proposals, func(p hq.Proposal) bool { return p.ID == id })
	if !isProposal(s) || i < 0 {
		return hq.Proposal{}, false
	}
	return m.deckk.proposals[i], true
}

// openProposals are the proposals the user hasn't started or dismissed.
func (m Model) openProposals() []claude.Session {
	var list []claude.Session
	for _, p := range m.deckk.proposals {
		s := proposalSession(p)
		if at, done := m.store.Dismissed[s.ID]; !done || !at.Equal(p.At) {
			list = append(list, s)
		}
	}
	return list
}

// A prompt waits to be typed into a session that is starting, once Claude
// in it is ready. It gives up if no session turns up.
type awaitingPrompt struct {
	key       string
	sessionID string
	dir       string
	after     time.Time
	prompt    string
}

const awaitFor = 2 * time.Minute

const openPrefix = "open:"

// begin starts a proposed session, or resumes a stopped one, and queues the
// prompt for when Claude is up.
func (m *Model) begin(s claude.Session, sg hq.Suggestion) tea.Cmd {
	now := time.Now()
	wait := awaitingPrompt{key: s.ID, sessionID: s.ID, dir: s.Dir, after: now, prompt: sg.Prompt}
	var open tea.Cmd
	if p, ok := m.proposal(s); ok {
		wait.sessionID = ""
		open = m.newSession(p.Folder, false)
		m.report([]hq.Event{{At: now, Title: s.Title, Kind: "was started from your proposal", Detail: p.ID}})
	} else {
		open = m.open(s)
	}
	m.mark(m.store.Dismiss(s.ID, sg.At))
	if m.cfg.Prefill() {
		m.deckk.awaiting = append(m.deckk.awaiting, wait)
	}
	return open
}

// fillStarted types queued prompts into sessions that have come up.
func (m *Model) fillStarted() tea.Cmd {
	now := time.Now()
	var cmds []tea.Cmd
	m.deckk.awaiting = slices.DeleteFunc(m.deckk.awaiting, func(w awaitingPrompt) bool { return now.Sub(w.after) > awaitFor })
	for _, w := range m.deckk.awaiting {
		i := slices.IndexFunc(m.sessions, func(s claude.Session) bool { return w.matches(s) })
		if i < 0 || deck.StateOf(m.sessions[i]) != deck.YourTurn {
			continue
		}
		if _, hosted := m.window(m.sessions[i]); hosted {
			cmds = append(cmds, m.deliver(delivery{key: openPrefix + w.key, target: m.sessions[i].TTY, text: w.prompt}))
		}
	}
	return tea.Batch(cmds...)
}

// A resumed session keeps its ID; a new one is known by its folder and by
// starting after the user asked for it.
func (w awaitingPrompt) matches(s claude.Session) bool {
	if w.sessionID != "" {
		return s.ID == w.sessionID && s.Running()
	}
	return s.Running() && filepath.Clean(s.Cwd) == filepath.Clean(w.dir) && !s.StartedAt.Before(w.after.Add(-5*time.Second))
}

func (m *Model) filled(key string) {
	m.deckk.awaiting = slices.DeleteFunc(m.deckk.awaiting, func(w awaitingPrompt) bool { return openPrefix+w.key == key })
}
