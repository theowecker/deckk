package main

import (
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/config"
	"github.com/theowecker/deckk/internal/hq"
	"github.com/theowecker/deckk/internal/terminal"
	"github.com/theowecker/deckk/internal/tmux"
	"github.com/theowecker/deckk/internal/web"
)

type check struct {
	ok       bool
	optional bool
	good     string
	bad      string
	fixIt    string
}

// doctor checks what deckk depends on and says how to fix what's wrong.
// Optional checks only cost a feature, so they don't fail it.
func doctor(out io.Writer, cfg *config.Config, cfgErr error) bool {
	var checks []check
	add := func(c check) { checks = append(checks, c) }

	if v, err := commandVersion("tmux", "-V"); err != nil {
		add(check{bad: "tmux isn't installed", fixIt: "brew install tmux"})
	} else {
		add(check{ok: true, good: v})
		if tmux.HasSession() {
			server, installed, stale := tmux.Stale()
			add(check{ok: !stale, good: "deckk's tmux server matches", bad: "deckk's tmux server still runs tmux " + server,
				fixIt: "Run deckk and answer y to restart it on tmux " + installed})
		}
	}
	if v, err := commandVersion("claude", "--version"); err != nil {
		add(check{bad: "Claude Code isn't on your PATH", fixIt: "Install Claude Code, then run deckk from a new terminal"})
	} else {
		add(check{ok: true, good: "Claude Code " + strings.TrimSuffix(v, " (Claude Code)")})
	}
	_, gitErr := exec.LookPath("git")
	add(check{ok: gitErr == nil, optional: true, good: "git is installed", bad: "git isn't installed",
		fixIt: "Without it deckk can't show branches, changes or reviews"})
	_, ghErr := exec.LookPath("gh")
	ghAuthed := ghErr == nil && exec.Command("gh", "auth", "status").Run() == nil
	add(check{ok: ghAuthed, optional: true, good: "gh is signed in, so deckk can follow PRs", bad: "deckk can't read PR checks or reviews",
		fixIt: "brew install gh, then gh auth login"})
	_, notifierErr := exec.LookPath("terminal-notifier")
	add(check{ok: notifierErr == nil, optional: true, good: "Clicking a notification opens its session",
		bad: "Clicking a notification opens Script Editor", fixIt: "brew install terminal-notifier"})

	if !tmux.Inside() {
		app, ok := terminal.Current()
		add(check{ok: ok, optional: true, good: "deckk can open and find tabs in " + app.Name,
			bad: "deckk can't open or find tabs in this terminal", fixIt: "It runs fine here; o and jumping to other tabs need iTerm2, Ghostty 1.3+ or Terminal"})
	}
	if cfgErr != nil {
		add(check{bad: "The config file doesn't read", fixIt: fmt.Sprintf("Fix %s: %v", config.DefaultPath(), cfgErr)})
	} else {
		add(check{ok: true, good: "The config file reads cleanly"})
		if cfg.WebOn() && tmux.HasSession() {
			add(check{ok: hubAnswers(), optional: true, good: "The browser hub answers", bad: "The browser hub isn't answering on 127.0.0.1:4747",
				fixIt: "Something else may hold the port; press w in deckk to open the hub wherever it is"})
		}
		if cfg.HQOn() {
			add(check{ok: claude.Trusted(hq.Dir()), optional: true, good: "Claude trusts the deckk session's folder",
				bad: "Claude hasn't been told it may work in the deckk session's folder", fixIt: "Press c in deckk and accept Claude's trust question"})
		}
	}

	healthy := true
	for _, c := range checks {
		switch {
		case c.ok:
			fmt.Fprintln(out, "✓ "+c.good)
		case c.optional:
			fmt.Fprintln(out, "⚠ "+c.bad+"\n  "+c.fixIt)
		default:
			healthy = false
			fmt.Fprintln(out, "✗ "+c.bad+"\n  "+c.fixIt)
		}
	}
	return healthy
}

func commandVersion(name string, arg string) (string, error) {
	out, err := exec.Command(name, arg).Output()
	return strings.TrimSpace(string(out)), err
}

func hubAnswers() bool {
	client := http.Client{Timeout: time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", web.Port))
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
