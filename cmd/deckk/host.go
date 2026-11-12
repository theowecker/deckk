package main

import (
	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/terminal"
	"github.com/theowecker/deckk/internal/tmux"
)

type host struct {
	tmux.Server
	tmuxPath string
	app      terminal.App
}

func newHost(tmuxPath, program string) host {
	app, _ := terminal.Lookup(program)
	return host{tmuxPath: tmuxPath, app: app}
}

func (host) Focus(tty string) error { return terminal.Focus(tty) }

// Closing the tab is a nicety; failing it must not stop the resume.
func (host) Release(pid int, tty string) error {
	if err := claude.Stop(pid); err != nil {
		return err
	}
	_ = terminal.Close(tty)
	return nil
}

func (h host) PopOut(windowID string) error {
	if !h.app.CanOpenTabs() {
		return terminal.ErrUnsupported
	}
	session, err := tmux.PopOut(windowID)
	if err != nil {
		return err
	}
	if err := h.app.OpenTab(tmux.AttachCommand(h.tmuxPath, session)); err != nil {
		_ = tmux.BringBack(windowID)
		return err
	}
	return nil
}

func (host) BringBack(windowID string) error { return tmux.BringBack(windowID) }

func (host) MoveAfter(windowID, afterID string) error { return tmux.MoveAfter(windowID, afterID) }

func (host) SetTabKeys(preset string) error {
	if err := tmux.BindTabKeys(preset); err != nil {
		return err
	}
	return terminal.RefreshProfile(preset)
}
