package main

import (
	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/iterm"
	"github.com/theowecker/deckk/internal/tmux"
)

type host struct {
	tmux.Server
	tmuxPath string
}

func (host) Focus(tty string) error { return iterm.Focus(tty) }

// Closing the tab is a nicety; failing it must not stop the resume.
func (host) Release(pid int, tty string) error {
	if err := claude.Stop(pid); err != nil {
		return err
	}
	_ = iterm.Close(tty)
	return nil
}

func (h host) PopOut(windowID string) error {
	session, err := tmux.PopOut(windowID)
	if err != nil {
		return err
	}
	if err := iterm.OpenTab(tmux.AttachCommand(h.tmuxPath, session)); err != nil {
		_ = tmux.BringBack(windowID)
		return err
	}
	return nil
}

func (host) BringBack(windowID string) error { return tmux.BringBack(windowID) }
