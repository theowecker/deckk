package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/config"
	"github.com/theowecker/deckk/internal/deck"
	"github.com/theowecker/deckk/internal/hq"
)

type reviewDoneMsg struct {
	session claude.Session
	review  hq.Review
	err     error
	quiet   bool
}

func (m *Model) review(s claude.Session, quiet bool) tea.Cmd { return m.startReview(s, quiet, false) }

// pressE shows a session's current review in full, and runs a fresh one when
// it is already showing or there is none to show.
func (m *Model) pressE(s claude.Session) tea.Cmd {
	r, ok := m.deckk.reviews[s.ID]
	current := ok && !s.UpdatedAt.After(r.At)
	if current && m.reviewOpen != s.ID {
		m.reviewOpen = s.ID
		m.setFlash("Press e again to run a fresh review", nil)
		return nil
	}
	m.reviewOpen = s.ID
	return m.startReview(s, false, current)
}

func (m *Model) startReview(s claude.Session, quiet, fresh bool) tea.Cmd {
	if m.reviewing[s.ID] {
		if !quiet {
			m.setFlash("Already reviewing "+displayTitle(s), nil)
		}
		return nil
	}
	m.reviewing[s.ID] = true
	if !quiet {
		m.setFlash("Reviewing "+displayTitle(s)+"…", nil)
	}
	note, _, _ := m.note(s)
	return func() tea.Msg {
		r, err := hq.ReviewSession(context.Background(), s, note.Text, fresh)
		return reviewDoneMsg{s, r, err, quiet}
	}
}

func (m *Model) reviewed(msg reviewDoneMsg) {
	delete(m.reviewing, msg.session.ID)
	title := displayTitle(msg.session)
	switch {
	case errors.Is(msg.err, hq.ErrNothingToReview):
		if !msg.quiet {
			m.setFlash(title+" has no changes to review", nil)
		}
		return
	case msg.err != nil:
		if !msg.quiet {
			m.setFlash("", msg.err)
		}
		return
	}
	m.deckk.reviews[msg.session.ID] = msg.review
	found := reviewFinding(msg.review)
	m.report([]hq.Event{{At: msg.review.At, Session: msg.session.ID, Title: title, Kind: hq.Reviewed, Detail: found}})
	if !msg.quiet {
		m.setFlash("Review of "+title+": "+found, nil)
	}
}

func reviewFinding(r hq.Review) string {
	if r.OK {
		return "nothing to flag"
	}
	concerns := concernLines(r.Text)
	switch len(concerns) {
	case 0:
		return "found concerns"
	case 1:
		return "found 1 concern: " + concerns[0]
	}
	return fmt.Sprintf("found %d concerns, first: %s", len(concerns), concerns[0])
}

// A review that found concerns in a session the user isn't looking at is
// worth a notification, since nothing else would bring them to it.
func (m *Model) notifyReview(msg reviewDoneMsg) tea.Cmd {
	if msg.err != nil || msg.review.OK || !m.cfg.NotifyOn() || m.watching(msg.session) {
		return nil
	}
	s, title := msg.session, displayTitle(msg.session)+": review found concerns"
	body := strings.TrimPrefix(reviewFinding(msg.review), "found ")
	return func() tea.Msg {
		if err := m.host.Notify(s.ID, title, s.Project(), body); err != nil {
			return flashMsg{err: err}
		}
		return nil
	}
}

// autoReview queues sessions as they finish or open a PR and reviews them one
// at a time, so a burst of finished sessions doesn't start a burst of reviews.
func (m *Model) autoReview(events []hq.Event) tea.Cmd {
	mode := m.cfg.Reviews
	if mode == config.ReviewManual {
		return nil
	}
	for _, e := range events {
		worth := e.Kind == hq.OpenedPR
		if e.Kind == hq.Finished {
			s, _ := m.sessionByID(e.Session)
			worth = mode == config.ReviewAuto || hq.SoundsDone(s.LastReply)
		}
		if worth && !slices.Contains(m.reviewQueue, e.Session) {
			m.reviewQueue = append(m.reviewQueue, e.Session)
		}
	}
	return m.nextReview()
}

func (m *Model) nextReview() tea.Cmd {
	for len(m.reviewQueue) > 0 && len(m.reviewing) == 0 {
		id := m.reviewQueue[0]
		m.reviewQueue = m.reviewQueue[1:]
		if s, ok := m.sessionByID(id); ok && (s.Git.Changed > 0 || s.Git.Ahead > 0 || s.PR != "") {
			return m.review(s, true)
		}
	}
	return nil
}

func (m Model) reviewSummary(s claude.Session) (mark, line string, ok bool) {
	if m.reviewing[s.ID] {
		return ghost.Render("◌"), ghost.Render("◌ reviewing…"), true
	}
	r, found := m.deckk.reviews[s.ID]
	if !found {
		if n := s.Git.Changed; n > 0 && !isProposal(s) {
			files := "files"
			if n == 1 {
				files = "file"
			}
			return "", ghost.Render(fmt.Sprintf("e reviews %d changed %s", n, files)), true
		}
		return "", "", false
	}
	later := ""
	if s.UpdatedAt.After(r.At) {
		later = ghost.Render(" · from before its last turn")
	}
	if r.OK {
		return dim.Render("✓"), dim.Render("✓ review found nothing to flag") + later, true
	}
	concerns := concernLines(r.Text)
	first := "review found concerns"
	if len(concerns) > 0 {
		first = concerns[0]
	}
	more := ""
	if len(concerns) > 1 {
		more = ghost.Render(fmt.Sprintf(" +%d more", len(concerns)-1))
	}
	return warn.Render("⚠"), warn.Render("⚠ "+first) + more + later, true
}

func concernLines(text string) []string {
	var out []string
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "-*• "))
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// reviewLines are the selected session's review in full, once e opened it.
func (m Model) reviewLines(s claude.Session, room int) []string {
	r, ok := m.deckk.reviews[s.ID]
	if m.reviewOpen != s.ID || !ok || m.reviewing[s.ID] {
		return nil
	}
	when := "reviewed " + deck.Ago(r.At, time.Now()) + " ago"
	if s.UpdatedAt.After(r.At) {
		when += ", before its last turn"
	}
	if r.OK {
		return []string{dim.Render("✓ Nothing to flag") + ghost.Render(" · "+when+" · e reviews again")}
	}
	lines := []string{warn.Render("⚑ The review flagged") + ghost.Render(" · "+when+" · e reviews again")}
	for _, c := range concernLines(r.Text) {
		lines = append(lines, "  "+soft.Render(ansi.Truncate("• "+c, room-2, "…")))
	}
	return lines
}
