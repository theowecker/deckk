package main

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/config"
	"github.com/theowecker/deckk/internal/hq"
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

func (host) SetRestore(n int) error { return tmux.SetRestore(n) }

func (host) LayoutHQ(show, expanded bool) error { return tmux.LayoutHQ(show, expanded) }

func (host) SetTabKeys(preset string) error {
	if err := tmux.BindTabKeys(preset); err != nil {
		return err
	}
	return terminal.RefreshProfile(preset)
}

// Writes the role and tool config first so an upgraded deckk always hands it
// the current ones.
func (h host) OpenHQ(cfg config.Config, focus bool) error {
	if err := openHQ(&cfg); err != nil {
		return err
	}
	if focus {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		return tmux.OpenHQ(hq.Dir(), hq.Command(&cfg, exe), true)
	}
	return nil
}

func openHQ(cfg *config.Config) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := hq.Setup(exe); err != nil {
		return err
	}
	return tmux.OpenHQ(hq.Dir(), hq.Command(cfg, exe), false)
}

func (host) CloseHQ() error { return tmux.CloseHQ() }

func (h host) Notify(id, title, subtitle, body string) error {
	n := terminal.Notification{Title: title, Subtitle: subtitle, Body: body, Group: "deckk-" + id}
	if exe, err := stableExecutable(); err == nil {
		n.OnClick = shellQuote(exe) + " focus " + shellQuote(id)
	}
	return h.app.Notify(n)
}

func (host) HQPane() (tmux.Pane, bool, error) { return tmux.HQ() }

func (host) Deliver(target, text, replace string, send bool) (tmux.Delivery, error) {
	return tmux.Deliver(target, text, replace, send)
}

// A notification can be clicked after an upgrade has removed the versioned
// path deckk runs from, so it calls deckk through the PATH when it can.
func stableExecutable() (string, error) {
	if path, err := exec.LookPath("deckk"); err == nil {
		if abs, err := filepath.Abs(path); err == nil {
			return abs, nil
		}
	}
	return os.Executable()
}
