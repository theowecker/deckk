package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/config"
	"github.com/theowecker/deckk/internal/hq"
)

type reviewDoneMsg struct {
	session claude.Session
	review  hq.Review
	err     error
	quiet   bool
}

func (m *Model) review(s claude.Session, quiet bool) tea.Cmd {
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
		r, err := hq.ReviewSession(context.Background(), s, note.Text)
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
	if msg.quiet {
		return
	}
	if msg.review.OK {
		m.setFlash("Review of "+title+": nothing stands out", nil)
	} else {
		m.setFlash("Review of "+title+" found concerns", nil)
	}
}

// autoReview queues sessions as they finish or open a PR and reviews them one
// at a time, so a burst of finished sessions doesn't start a burst of reviews.
func (m *Model) autoReview(events []hq.Event) tea.Cmd {
	mode := m.cfg.Reviews
	if mode == "" {
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
