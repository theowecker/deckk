package hq

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/deck"
	"github.com/theowecker/deckk/internal/git"
	"github.com/theowecker/deckk/internal/mcp"
	"github.com/theowecker/deckk/internal/tmux"
)

const (
	RoleHQ    = "hq"
	RoleChild = "child"
)

func Tools(role string) []mcp.Tool {
	if role == RoleHQ {
		return parentTools()
	}
	return childTools()
}

func ignoringContext(run func(mcp.Args) (string, error)) func(context.Context, mcp.Args) (string, error) {
	return func(_ context.Context, a mcp.Args) (string, error) { return run(a) }
}

// How long a session waits on ask_deckk before carrying on alone.
const askTimeout = 5 * time.Minute

func parentTools() []mcp.Tool {
	sessionParam := mcp.Param{Name: "session", Kind: "string", Description: "Session ID, or its first 8 characters as list_sessions shows them", Required: true}
	return []mcp.Tool{
		{
			Name:        "list_sessions",
			Description: "List the user's Claude Code sessions: running ones, and with include_recent the ones from the last few days. Each has its ID, state, folder, branch, PR, any pending suggestion and its last exchange.",
			Params:      []mcp.Param{{Name: "include_recent", Kind: "boolean", Description: "Also list sessions that stopped in the last three days"}},
			Run:         ignoringContext(listSessions),
		},
		{
			Name:        "read_session",
			Description: "Read the last few prompts and replies of one session.",
			Params:      []mcp.Param{sessionParam, {Name: "turns", Kind: "integer", Description: "How many recent exchanges, 1 to 10. Defaults to 3"}},
			Run:         ignoringContext(readSession),
		},
		{
			Name: "remember_session",
			Description: "Keep a note about a session: what it is for and where it stands, in a sentence or two. list_sessions shows your note and whether the session moved since, " +
				"so you only reread sessions that changed. An empty note forgets it.",
			Params: []mcp.Param{sessionParam, {Name: "note", Kind: "string", Description: "What the session is for and where it stands", Required: true}},
			Run: ignoringContext(func(a mcp.Args) (string, error) {
				id, err := resolve(a.String("session"))
				if err != nil {
					return "", err
				}
				return "Noted.", Remember(id, strings.Join(strings.Fields(a.String("note")), " "))
			}),
		},
		{
			Name:        "read_inbox",
			Description: "What changed since you last looked: sessions that finished a turn, opened a PR or closed, notes sessions left you, and questions sessions are waiting on you to answer.",
			Run:         ignoringContext(inboxTool),
		},
		{
			Name:        "suggest",
			Description: "Propose the next prompt for a session. For a waiting session, deckk types it into the session's prompt box when the user is not looking at it. For a stopped session, deckk offers to resume it with this prompt. Replaces any earlier suggestion for that session.",
			Params: []mcp.Param{
				sessionParam,
				{Name: "prompt", Kind: "string", Description: "One line, written as the user would type it to that session", Required: true},
				{Name: "why", Kind: "string", Description: "A few words on why, shown to the user"},
				{Name: "safe_to_send", Kind: "boolean", Description: "True only if sending it without review could not hurt anything. It is still only sent if the user allows that"},
			},
			Run: ignoringContext(suggest),
		},
		{
			Name:        "clear_suggestion",
			Description: "Withdraw a suggestion that no longer makes sense.",
			Params:      []mcp.Param{sessionParam},
			Run: ignoringContext(func(a mcp.Args) (string, error) {
				id, err := resolve(a.String("session"))
				if err != nil {
					return "", err
				}
				return "Cleared.", ClearSuggestion(id)
			}),
		},
		{
			Name: "propose_session",
			Description: "Propose starting a new session: a folder and the first prompt for it. The user sees it under Next up and starts it with one key; deckk types the prompt in for them to send. " +
				"Use it for work that belongs in its own session, such as a separate fix or a project nothing is running in. To pick an old session back up, suggest to it instead.",
			Params: []mcp.Param{
				{Name: "folder", Kind: "string", Description: "Where to start Claude, an absolute path or one starting with ~/", Required: true},
				{Name: "prompt", Kind: "string", Description: "The first prompt, one line, as the user would type it", Required: true},
				{Name: "why", Kind: "string", Description: "A few words on why, shown to the user"},
			},
			Run: ignoringContext(propose),
		},
		{
			Name:        "withdraw_proposal",
			Description: "Take back a proposed session that no longer makes sense.",
			Params:      []mcp.Param{{Name: "proposal", Kind: "string", Description: "The proposal ID from list_sessions", Required: true}},
			Run: ignoringContext(func(a mcp.Args) (string, error) {
				return "Withdrawn.", Withdraw(a.String("proposal"))
			}),
		},
		{
			Name:        "answer",
			Description: "Answer a question a session asked you with ask_deckk. The session is blocked until you do.",
			Params: []mcp.Param{
				{Name: "question", Kind: "string", Description: "The question ID from read_inbox", Required: true},
				{Name: "answer", Kind: "string", Description: "What the session should do next and why, in a few sentences", Required: true},
			},
			Run: ignoringContext(func(a mcp.Args) (string, error) {
				return "Sent.", Answer(a.String("question"), a.String("answer"))
			}),
		},
		{
			Name:        "read_goals",
			Description: "Read the user's goals file.",
			Run: ignoringContext(func(mcp.Args) (string, error) {
				goals, err := ReadGoals()
				if goals == "" && err == nil {
					return "No goals written yet.", nil
				}
				return goals, err
			}),
		},
		{
			Name:        "write_goals",
			Description: "Replace the user's goals file. Markdown. Keep it short: goals, what each is waiting on, and which sessions serve it.",
			Params:      []mcp.Param{{Name: "content", Kind: "string", Description: "The whole new file", Required: true}},
			Run: ignoringContext(func(a mcp.Args) (string, error) {
				return "Saved.", WriteGoals(a.String("content"))
			}),
		},
	}
}

func childTools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name: "ask_deckk",
			Description: "Ask deckk, the coordinator that sees all of the user's sessions, projects and goals, what to do next or how this work fits with the rest. " +
				"Use it when you finish a task and the next step isn't obvious, or when work elsewhere might change your plan. Waits up to five minutes for an answer.",
			Params: []mcp.Param{{Name: "question", Kind: "string", Description: "What you did and what you want to know", Required: true}},
			Run:    ask,
		},
		{
			Name:        "tell_deckk",
			Description: "Leave deckk, the coordinator of the user's sessions, a short note it should know: you are blocked, you finished something others depend on, or you changed something shared. Doesn't wait.",
			Params:      []mcp.Param{{Name: "note", Kind: "string", Description: "One or two sentences", Required: true}},
			Run: ignoringContext(func(a mcp.Args) (string, error) {
				_, err := fromThisSession(a.String("note"), false)
				return "Noted.", err
			}),
		},
	}
}

func ask(ctx context.Context, a mcp.Args) (string, error) {
	if !Running() {
		return "deckk isn't running, so nobody can answer. Carry on with your best judgement.", nil
	}
	q, err := fromThisSession(a.String("question"), true)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, askTimeout)
	defer cancel()
	if answer, ok := Await(ctx, q.ID); ok {
		return answer, nil
	}
	return "deckk did not answer in time. Carry on with your best judgement.", nil
}

func Running() bool {
	live, err := claude.ReadLive(filepath.Join(claude.Home(), "sessions"))
	if err != nil {
		return false
	}
	for _, l := range live {
		if Is(l.Cwd) {
			return true
		}
	}
	return false
}

// The server's parent process is the claude that started it.
func fromThisSession(text string, wait bool) (Question, error) {
	if strings.TrimSpace(text) == "" {
		return Question{}, errors.New("nothing to send")
	}
	q := Question{ID: newID(), Text: text, Wait: wait, At: time.Now(), Title: "a session"}
	if live, err := claude.ReadLive(filepath.Join(claude.Home(), "sessions")); err == nil {
		for _, l := range live {
			if l.PID == os.Getppid() {
				q.Session = l.SessionID
				q.Title = sessionTitle(l.SessionID, l.Cwd)
			}
		}
	}
	return q, Post(q)
}

func sessionTitle(id, cwd string) string {
	if t, err := claude.ReadTranscript(claude.TranscriptPath(claude.Home(), id)); err == nil {
		if title := t.Session().Title; title != "" {
			return title
		}
	}
	return filepath.Base(cwd)
}

func listSessions(a mcp.Args) (string, error) {
	sessions, err := claude.Load(claude.Home(), time.Now().AddDate(0, 0, -3), nil)
	if err != nil {
		return "", err
	}
	suggestions, _ := LoadSuggestions()
	notes, _ := LoadNotes()
	where := windows()
	now := time.Now()
	sessions = slices.DeleteFunc(sessions, func(s claude.Session) bool {
		return Is(s.Cwd) || s.Background || !s.Running() && !a.Bool("include_recent")
	})
	collisions := deck.Collisions(sessions, claude.EditsOf(sessions), git.Root)
	slices.SortFunc(sessions, func(x, y claude.Session) int { return y.UpdatedAt.Compare(x.UpdatedAt) })
	var b strings.Builder
	if len(sessions) == 0 {
		b.WriteString("No sessions.\n")
	}
	for _, s := range sessions {
		fmt.Fprintf(&b, "- [%s] %s\n  %s\n", claude.ShortID(s.ID), s.Title, describe(s, where, now))
		note, noted := notes[s.ID]
		if noted {
			moved := "unchanged since"
			if s.UpdatedAt.After(note.At) {
				moved = "moved on since"
			}
			fmt.Fprintf(&b, "  your note (%s): %s\n", moved, note.Text)
		}
		// A note on a session that hasn't moved already says what matters.
		if !noted || s.UpdatedAt.After(note.At) {
			if s.LastPrompt != "" {
				fmt.Fprintf(&b, "  last prompt: %s\n", clip(s.LastPrompt, 200))
			}
			if s.LastReply != "" {
				fmt.Fprintf(&b, "  last reply: %s\n", clip(s.LastReply, 300))
			}
		}
		for _, c := range deck.CollisionsOf(collisions, s.ID) {
			other := c.Other(s.ID)
			if c.Clash() {
				fmt.Fprintf(&b, "  COLLISION: [%s] %s also edited %s\n", claude.ShortID(other.ID), other.Title, strings.Join(c.Files, ", "))
			} else {
				fmt.Fprintf(&b, "  shares the checkout %s with [%s] %s\n", c.Checkout, claude.ShortID(other.ID), other.Title)
			}
		}
		if sg, ok := suggestions[s.ID]; ok && !sg.At.Before(s.StatusAt) {
			fmt.Fprintf(&b, "  your suggestion: %s\n", sg.Prompt)
		}
	}
	if proposals, _ := LoadProposals(); len(proposals) > 0 {
		b.WriteString("\nProposed sessions not started yet:\n")
		for _, p := range proposals {
			fmt.Fprintf(&b, "- [%s] in %s: %s\n", p.ID, p.Folder, p.Prompt)
		}
	}
	return b.String(), nil
}

func describe(s claude.Session, where map[string]string, now time.Time) string {
	parts := []string{s.Project()}
	if s.Branch != "" && s.Branch != "HEAD" {
		parts = append(parts, "branch "+s.Branch)
	}
	switch deck.StateOf(s) {
	case deck.Working:
		parts = append(parts, "working")
	case deck.YourTurn:
		parts = append(parts, "waiting on the user for "+deck.For(s.StatusAt, now))
	case deck.Stopped:
		parts = append(parts, "stopped "+deck.Ago(s.UpdatedAt, now)+" ago")
	default:
		parts = append(parts, s.Status)
	}
	if s.Running() {
		parts = append(parts, cmp.Or(where[s.TTY], "in another terminal tab"))
		if s.Peer != "" {
			parts = append(parts, "message it as "+s.Peer)
		}
	}
	if s.PR != "" {
		parts = append(parts, "PR "+s.PR)
	}
	if s.Git.Changed > 0 {
		parts = append(parts, fmt.Sprintf("%d uncommitted changes", s.Git.Changed))
	}
	return strings.Join(parts, " · ")
}

func windows() map[string]string {
	where := map[string]string{}
	list, err := tmux.Windows()
	if err != nil {
		return where
	}
	for _, w := range list {
		if w.PoppedOut() {
			where[w.TTY] = "in deckk, popped out to its own tab"
		} else if !w.Dashboard {
			where[w.TTY] = "in deckk"
		}
	}
	return where
}

func readSession(a mcp.Args) (string, error) {
	id, err := resolve(a.String("session"))
	if err != nil {
		return "", err
	}
	path := claude.TranscriptPath(claude.Home(), id)
	t, err := claude.ReadTranscript(path)
	if err != nil {
		return "", err
	}
	turns, err := claude.Exchanges(path, min(max(a.Int("turns", 3), 1), 10))
	if err != nil {
		return "", err
	}
	s := t.Session()
	var b strings.Builder
	fmt.Fprintf(&b, "%s [%s] in %s", s.Title, claude.ShortID(id), s.Cwd)
	if s.PR != "" {
		fmt.Fprintf(&b, ", PR %s", s.PRURL)
	}
	b.WriteString("\n")
	for _, turn := range turns {
		fmt.Fprintf(&b, "\nUser: %s\nClaude: %s\n", clip(turn.Prompt, 1500), clip(turn.Reply, 2500))
	}
	return b.String(), nil
}

func inboxTool(mcp.Args) (string, error) {
	events, err := Unread()
	if err != nil {
		return "", err
	}
	questions, err := Questions()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, e := range events {
		fmt.Fprintf(&b, "- %s [%s] %s\n", e.At.Format("15:04"), claude.ShortID(e.Session), e.Line())
	}
	for _, q := range questions {
		if q.Wait {
			fmt.Fprintf(&b, "- QUESTION %s from %s [%s], waiting for your answer: %s\n", q.ID, q.Title, claude.ShortID(q.Session), q.Text)
		} else {
			fmt.Fprintf(&b, "- note from %s [%s]: %s\n", q.Title, claude.ShortID(q.Session), q.Text)
		}
	}
	Delivered(questions)
	if b.Len() == 0 {
		return "Nothing new.", nil
	}
	return b.String(), nil
}

func suggest(a mcp.Args) (string, error) {
	id, err := resolve(a.String("session"))
	if err != nil {
		return "", err
	}
	prompt := strings.Join(strings.Fields(a.String("prompt")), " ")
	if prompt == "" {
		return "", errors.New("the prompt is empty")
	}
	sg := Suggestion{Prompt: prompt, Why: a.String("why"), Safe: a.Bool("safe_to_send"), At: time.Now()}
	return "Suggested. deckk will put it in that session's prompt when it can.", Suggest(id, sg)
}

func propose(a mcp.Args) (string, error) {
	folder := a.String("folder")
	if rest, ok := strings.CutPrefix(folder, "~/"); ok {
		home, _ := os.UserHomeDir()
		folder = filepath.Join(home, rest)
	}
	if info, err := os.Stat(folder); err != nil || !info.IsDir() || !filepath.IsAbs(folder) {
		return "", fmt.Errorf("%s is not a folder", a.String("folder"))
	}
	prompt := strings.Join(strings.Fields(a.String("prompt")), " ")
	if prompt == "" {
		return "", errors.New("the prompt is empty")
	}
	p := Proposal{ID: newID(), Folder: filepath.Clean(folder), Prompt: prompt, Why: a.String("why"), At: time.Now()}
	return "Proposed as " + p.ID + ". The user can start it from Next up.", Propose(p)
}

func resolve(prefix string) (string, error) {
	prefix = strings.TrimSpace(strings.Trim(prefix, "[]"))
	if len(prefix) < 4 {
		return "", errors.New("give at least the first 8 characters of the session ID")
	}
	paths, _ := filepath.Glob(filepath.Join(claude.Home(), "projects", "*", prefix+"*.jsonl"))
	ids := map[string]bool{}
	for _, p := range paths {
		ids[strings.TrimSuffix(filepath.Base(p), ".jsonl")] = true
	}
	switch len(ids) {
	case 0:
		return "", fmt.Errorf("no session starts with %s", prefix)
	case 1:
		for id := range ids {
			return id, nil
		}
	}
	return "", fmt.Errorf("%s matches more than one session; use more of the ID", prefix)
}

func clip(s string, n int) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}
