package ui

import (
	"maps"
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
	if !m.cfg.HQOn() {
		return nil
	}
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

// begin starts a proposed session, or resumes a stopped one, and queues the
// prompt for when Claude is up.
func (m *Model) begin(s claude.Session, sg hq.Suggestion) tea.Cmd {
	now := time.Now()
	wait := awaitingPrompt{key: s.ID, sessionID: s.ID, dir: s.Dir, after: now, prompt: sg.Prompt}
	var open tea.Cmd
	if p, ok := m.proposal(s); ok {
		wait.sessionID = ""
		// The deckk session reads this in its inbox; it isn't worth a turn.
		_ = hq.AppendEvents([]hq.Event{{At: now, Title: s.Title, Kind: "was started from your proposal", Detail: p.ID}})
		open = tea.Batch(m.newSession(p.Folder, false), withdraw(p.ID))
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
	claimed := maps.Clone(m.deckk.given)
	for _, w := range m.deckk.awaiting {
		claimed[w.sessionID] = true
	}
	for n := range m.deckk.awaiting {
		w := &m.deckk.awaiting[n]
		// Each new session takes one queued prompt, so two proposals for
		// one folder don't land in the same session.
		i := slices.IndexFunc(m.sessions, func(s claude.Session) bool {
			return w.matches(s) && (w.sessionID == s.ID || !claimed[s.ID])
		})
		if i < 0 {
			continue
		}
		if w.sessionID == "" {
			w.sessionID = m.sessions[i].ID
			claimed[w.sessionID] = true
		}
		if deck.StateOf(m.sessions[i]) != deck.YourTurn {
			continue
		}
		if _, hosted := m.window(m.sessions[i]); hosted {
			cmds = append(cmds, m.deliver(delivery{kind: opening, key: w.key, target: m.sessions[i].TTY, text: w.prompt}))
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

// filled drops a queued prompt once typed in. Its session stays taken, so
// another queued prompt for the same folder waits for a session of its own.
func (m *Model) filled(key string) {
	m.deckk.awaiting = slices.DeleteFunc(m.deckk.awaiting, func(w awaitingPrompt) bool {
		if w.key == key {
			m.deckk.given[w.sessionID] = true
		}
		return w.key == key
	})
}

// withdraw takes a proposal back once it is started or dismissed, so the
// deckk session no longer lists it as waiting to start.
func withdraw(id string) tea.Cmd {
	return func() tea.Msg {
		if err := hq.Withdraw(id); err != nil {
			return flashMsg{err: err}
		}
		return nil
	}
}
