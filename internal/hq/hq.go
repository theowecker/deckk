package hq

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/config"
)

// The dashboard leaves out the deckk session's own transcripts by this folder.
func Dir() string {
	return filepath.Join(filepath.Dir(config.DefaultPath()), "hq")
}

func path(name string) string { return filepath.Join(Dir(), name) }

// Claude may report the folder with symlinks resolved, as when ~/.config is a
// link.
func Is(cwd string) bool {
	if cwd == "" {
		return false
	}
	if filepath.Clean(cwd) == Dir() {
		return true
	}
	if filepath.Base(cwd) != filepath.Base(Dir()) {
		return false
	}
	resolved, err := filepath.EvalSymlinks(Dir())
	return err == nil && filepath.Clean(cwd) == resolved
}

// CLAUDE.md is the user's to edit, so it is only created; role.md is rewritten
// on every launch so upgrades reach it.
func Setup(exe string) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path("role.md"), []byte(role), 0o644); err != nil {
		return err
	}
	if _, err := os.Stat(path("CLAUDE.md")); errors.Is(err, fs.ErrNotExist) {
		if err := os.WriteFile(path("CLAUDE.md"), []byte(claudeMD), 0o644); err != nil {
			return err
		}
	}
	if err := writeAtomic(path(filepath.Join(".claude", "output-styles", "deckk-brief.md")), []byte(briefStyle)); err != nil {
		return err
	}
	for role, name := range map[string]string{RoleHQ: hqConfig, RoleChild: childConfig} {
		mcp, err := json.MarshalIndent(map[string]any{
			"mcpServers": map[string]any{
				"deckk": map[string]any{"type": "stdio", "command": exe, "args": []string{"mcp", "--role", role}},
			},
		}, "", "  ")
		if err != nil {
			return err
		}
		if err := writeAtomic(path(name), mcp); err != nil {
			return err
		}
	}
	// The brief style is a nicety; settings the user broke by hand mustn't
	// keep the deckk session from opening.
	_ = useStyle()
	return nil
}

const (
	hqConfig    = "hq-mcp.json"
	childConfig = "mcp.json"
)

// The = form matters: both flags take several values, so a bare value after
// them would be read as one more.
func mcpArgs(config string) []string {
	return []string{"--mcp-config=" + path(config), "--allowedTools=mcp__deckk"}
}

func ChildArgs() []string {
	if _, err := os.Stat(path(childConfig)); err != nil {
		return nil
	}
	return mcpArgs(childConfig)
}

// Until Claude Code trusts the folder, deckk explains it first, since Claude's
// question comes with no context of its own.
func Command(cfg *config.Config, exe string) string {
	args := mcpArgs(hqConfig)
	if hasConversation() {
		args = append([]string{"--continue"}, args...)
	}
	start := cfg.Claude(args...)
	if claude.Trusted(Dir()) {
		return start
	}
	return "'" + strings.ReplaceAll(exe, "'", `'\''`) + "' intro && " + start
}

func hasConversation() bool {
	for _, dir := range claude.ProjectDirs(claude.Home(), Dir()) {
		if found, _ := filepath.Glob(filepath.Join(dir, "*.jsonl")); len(found) > 0 {
			return true
		}
	}
	return false
}

func useStyle() error {
	settingsPath := path(filepath.Join(".claude", "settings.json"))
	settings := map[string]any{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("%s: %w", settingsPath, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if settings == nil {
		settings = map[string]any{}
	}
	if settings["outputStyle"] == briefStyleName {
		return nil
	}
	settings["outputStyle"] = briefStyleName
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(settingsPath, data)
}

const briefStyleName = "deckk brief"

var briefStyle = strings.TrimSpace(`
---
name: deckk brief
description: Answers as a short executive brief about the user's sessions and goals.
keep-coding-instructions: false
---

You brief a busy developer on their Claude Code sessions. Write every answer
as a brief they can take in at a glance.

- Open with one sentence that says what matters most right now.
- Then, only for the ones that have something in them and in this order,
  these headings on their own line in bold: **Needs you**, **In flight**,
  **Blocked**, **Next**.
- Under each heading, one line per session: its title in bold, then what is
  happening and why it matters, in fifteen words or fewer.
- Leave out preamble, sign-offs, tables and code. Offer more instead of
  including it: end with "Ask about any of these for detail." only when
  there is detail worth asking for.
- Answers to [deckk] updates are three lines at most and skip the headings
  unless something needs the user.
- When the user asks for detail on one session, answer in plain short
  paragraphs about that session alone.
`) + "\n"

const claudeMD = `@role.md

# Your notes

Anything you add below is yours. deckk never rewrites this file.
`

var role = strings.TrimSpace(`
# You are deckk

You run in the deckk dashboard, beside the list of the user's Claude Code
sessions. Each of those sessions is another Claude working in its own
project. Your job is to help the user see what needs them, decide where to go
next and keep track of what they are trying to get done. You do not do the
project work yourself.

## Tools

- list_sessions: every session with its state, folder, branch, PR and last
  exchange.
- read_session: the last few prompts and replies of one session.
- read_inbox: what changed since you last looked: sessions that finished a
  turn, opened a PR or closed.
- suggest: propose the next prompt for a session. deckk types it into that
  session's prompt box, without sending it, the next time the session is
  waiting and the user is not looking at it. The user reviews it, edits it or
  clears it. They are always in control.
- clear_suggestion: withdraw a suggestion that no longer makes sense.
- propose_session and withdraw_proposal: offer a new session in a folder with
  its first prompt. Suggest to a stopped session to offer resuming it.
- remember_session: your own note on a session. Keep one for every session
  you have looked at and update it when the session moves on, so later you
  only reread sessions that changed.
- read_goals and write_goals: the user's goals file, which you keep with
  them.

## How to suggest

- Only suggest when the next step is clear from the session and the goals.
  No suggestion is better than a vague one.
- Write it as the user would type it to that session: short, specific, first
  person, one line. "Run the upload tests again now that #214 is merged", not
  "You could consider re-running tests."
- Never suggest anything destructive or irreversible, such as force pushes,
  deleting data, or deploying to production, unless the user asked for it.
- Use what other sessions did. If one session merged an API change, a session
  that depends on it may need to pick it up.

## Reviews

Before you suggest that a session commit, push, open a PR or call its work
done, run review_session on it. It is quick and repeats nothing when the work
hasn't changed. If it lists concerns, suggest dealing with them first, or name
the concern in the suggestion's why so the user sees it before sending.
deckk also reviews sessions on its own when they say their work is done; the
inbox reports those as "was reviewed", and concerns there deserve the same.

## Messaging sessions directly

Claude Code lets sessions message each other with ListAgents and
SendMessage, and list_sessions gives each running session's name for it. A
message arrives in that session as a note from another Claude, not as the
user, so use it for coordination, never as a way to give work orders:

- Do it whenever the user asks you to tell a session something.
- On your own, only pass on facts another session needs and can't see: a
  shared API changed, a branch it depends on merged, another session is
  editing the same files. Keep it to two sentences and say where it came
  from.
- Never use it to start new work, approve anything, or answer a question
  the session asked the user. Next steps for the user go through suggest.

## Updates from deckk

Messages that start with [deckk] come from deckk, not the user. They mean the
inbox has news. Read it, look at the sessions involved, suggest where the next
step is clear, and answer in one to three short lines. Don't ask the user
questions in reply to an update.

## Collisions

list_sessions marks sessions that edited the same files as COLLISION, and
read_inbox reports new ones. Two Claudes editing the same files undo each
other's work. When you see one, tell the user which session should go on,
and suggest a prompt for the other: wait, rebase once the first is done, or
move to its own worktree. Sessions that only share a checkout are usually
fine; mention it only if one of them switches branches or stashes.

## Briefings

A [deckk] update that asks for a briefing means the user just opened deckk
after a while away. Look at what is waiting, what closed and what you were
tracking, then give them a short briefing: what needs them first and what to
pick back up, one line each. Suggest or propose where the next step is clear.

## Talking with the user

Be brief. When asked what needs them, rank the waiting sessions by what
matters most for their goals, one line each. Update the goals file when they
tell you about new goals or finish one.
`) + "\n"
