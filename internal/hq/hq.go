// Package hq is the deckk session: a Claude that runs beside the dashboard,
// watches the other sessions and suggests what to do next.
package hq

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/config"
)

// Dir is where the deckk session runs and keeps its files. Its transcripts
// are found by this folder, so the dashboard can leave them out.
func Dir() string {
	return filepath.Join(filepath.Dir(config.DefaultPath()), "hq")
}

func path(name string) string { return filepath.Join(Dir(), name) }

// Is reports whether a session is the deckk session itself.
func Is(cwd string) bool { return cwd != "" && filepath.Clean(cwd) == Dir() }

// Setup writes the deckk session's role and tool config. CLAUDE.md is the
// user's to edit, so it is only created; role.md belongs to deckk and is
// rewritten on every launch so upgrades reach it.
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

// ChildArgs give a session the tools to ask the deckk session what's next,
// once the deckk session has written their config.
func ChildArgs() []string {
	if _, err := os.Stat(path(childConfig)); err != nil {
		return nil
	}
	return mcpArgs(childConfig)
}

// Command starts the deckk session, picking up its last conversation when
// there is one.
func Command(cfg *config.Config) string {
	args := mcpArgs(hqConfig)
	if hasConversation() {
		args = append([]string{"--continue"}, args...)
	}
	return cfg.Claude(args...)
}

func hasConversation() bool {
	for _, dir := range claude.ProjectDirs(claude.Home(), Dir()) {
		if found, _ := filepath.Glob(filepath.Join(dir, "*.jsonl")); len(found) > 0 {
			return true
		}
	}
	return false
}

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
