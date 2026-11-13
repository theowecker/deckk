package ui

import (
	"strings"
	"time"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/github"
	"github.com/theowecker/deckk/internal/hq"
)

// PRs on sessions nobody has touched in a week are left alone, so a long
// history doesn't cost a GitHub call per refresh.
const prsFor = 7 * 24 * time.Hour

func prURLs(sessions []claude.Session, now time.Time) []string {
	var urls []string
	seen := map[string]bool{}
	for _, s := range sessions {
		if s.PRURL == "" || s.Background || seen[s.PRURL] || (!s.Running() && now.Sub(s.UpdatedAt) > prsFor) {
			continue
		}
		seen[s.PRURL] = true
		urls = append(urls, s.PRURL)
	}
	return urls
}

// trackPRs turns changes in a session's PR into events. The first look at a
// PR only learns where it stands, so opening deckk isn't news.
func (m *Model) trackPRs() []hq.Event {
	if m.prSeen == nil {
		m.prSeen = map[string]github.PR{}
	}
	now := time.Now()
	var events []hq.Event
	for _, s := range m.sessions {
		pr := s.PRStatus
		if s.PRURL == "" || pr.State == "" {
			continue
		}
		before, known := m.prSeen[s.PRURL]
		m.prSeen[s.PRURL] = pr
		if !known {
			continue
		}
		event := func(kind, detail string) {
			events = append(events, hq.Event{At: now, Session: s.ID, Title: displayTitle(s), Kind: kind, Detail: strings.TrimSpace(s.PR + " " + detail)})
		}
		switch {
		case pr.State == "merged" && before.State != "merged":
			event(hq.Merged, "")
		case pr.Checks == "failing" && before.Checks != "failing" && !pr.Done():
			event(hq.ChecksFailed, strings.Join(pr.Failing, ", "))
		case pr.Review == "changes requested" && before.Review != pr.Review:
			event(hq.ChangesRequested, "")
		case pr.Review == "approved" && before.Review != pr.Review:
			event(hq.Approved, "")
		}
	}
	return events
}

// prLabel says where a PR stands in a few words, and how much it matters:
// 2 needs the user, 1 is good news, 0 is just status.
func prLabel(pr github.PR) (string, int) {
	switch {
	case pr.State == "merged":
		return "merged", 1
	case pr.State == "closed":
		return "closed", 0
	case pr.Checks == "failing":
		return "checks failing", 2
	case pr.Review == "changes requested":
		return "changes requested", 2
	case pr.Review == "approved" && pr.Checks != "pending":
		return "approved", 1
	case pr.Checks == "pending":
		return "checks running", 0
	case pr.State == "draft":
		return "draft", 0
	case pr.Checks == "passing":
		return "checks pass", 1
	}
	return "", 0
}
