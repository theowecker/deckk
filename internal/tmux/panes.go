package tmux

import (
	"errors"
	"strconv"
	"strings"
)

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// The dashboard window holds two panes once the deckk session is open: the
// dashboard and, beside it, the deckk session. Each is marked with a pane
// option so either can be found whichever has focus.
const (
	dashboardPane = "dashboard"
	hqPane        = "hq"
)

// The deckk session goes beside the dashboard when the window is at least
// this wide, and under it otherwise.
const sideBySide = 150

type Pane struct {
	ID     string
	Role   string
	TTY    string
	Active bool
	// Seen is true when someone is looking at this pane: it has focus in the
	// window an attached client shows.
	Seen   bool
	InMode bool
}

func dashboardWindow() (string, error) {
	out, err := run("list-windows", "-t", "="+Session, "-F", "#{window_id}\t#{@deckk_dashboard}")
	if err != nil {
		return "", err
	}
	for line := range strings.Lines(out) {
		if id, mark, _ := strings.Cut(strings.TrimSpace(line), "\t"); mark == "1" {
			return id, nil
		}
	}
	return "", errors.New("the dashboard window is missing")
}

// DashboardPanes lists the dashboard window's panes. A pane from before panes
// were marked is the dashboard, since it was the only one.
func DashboardPanes() ([]Pane, error) {
	window, err := dashboardWindow()
	if err != nil {
		return nil, err
	}
	out, err := run("list-panes", "-t", window, "-F",
		"#{pane_id}\t#{@deckk_pane}\t#{pane_tty}\t#{pane_active}\t#{&&:#{pane_active},#{&&:#{window_active},#{session_attached}}}\t#{pane_in_mode}")
	if err != nil {
		return nil, err
	}
	var panes []Pane
	for line := range strings.Lines(out) {
		f := strings.Split(strings.TrimRight(line, "\n"), "\t")
		if len(f) != 6 {
			continue
		}
		panes = append(panes, Pane{ID: f[0], Role: f[1], TTY: f[2], Active: f[3] == "1", Seen: f[4] == "1", InMode: f[5] != "0"})
	}
	if len(panes) == 1 && panes[0].Role == "" {
		panes[0].Role = dashboardPane
	}
	return panes, nil
}

func findPane(role string) (Pane, bool, error) {
	panes, err := DashboardPanes()
	if err != nil {
		return Pane{}, false, err
	}
	for _, p := range panes {
		if p.Role == role {
			return p, true, nil
		}
	}
	return Pane{}, false, nil
}

// HQ is the deckk session's pane, if it is open.
func HQ() (Pane, bool, error) { return findPane(hqPane) }

func markPane(target, role string) error {
	_, err := run("set-option", "-p", "-t", target, "@deckk_pane", role)
	return err
}

// ErrDetached means nobody is attached yet, so the window's size, which
// decides the layout, isn't known.
var ErrDetached = errors.New("no client is attached")

// OpenHQ starts the deckk session beside the dashboard unless it is running.
func OpenHQ(dir, command string, focus bool) error {
	pane, open, err := HQ()
	if err != nil {
		return err
	}
	if !open {
		window, err := dashboardWindow()
		if err != nil {
			return err
		}
		if attached, _ := run("display-message", "-p", "-t", window, "#{session_attached}"); atoi(attached) == 0 {
			return ErrDetached
		}
		dash, found, err := findPane(dashboardPane)
		if err != nil {
			return err
		}
		if !found {
			return errors.New("the dashboard pane is missing")
		}
		layout := []string{"-v", "-l", "45%"}
		if width, err := run("display-message", "-p", "-t", dash.ID, "#{window_width}"); err == nil && atoi(width) >= sideBySide {
			layout = []string{"-h", "-l", "42%"}
		}
		args := append([]string{"split-window", "-d", "-P", "-F", "#{pane_id}", "-t", dash.ID, "-c", dir}, layout...)
		id, err := run(append(args, command)...)
		if err != nil {
			return err
		}
		if err := markPane(dash.ID, dashboardPane); err != nil {
			return err
		}
		if err := markPane(id, hqPane); err != nil {
			return err
		}
		pane.ID = id
	}
	if window, err := dashboardWindow(); err == nil {
		if err := titlePanes(window); err != nil {
			return err
		}
	}
	if !focus {
		return nil
	}
	window, err := dashboardWindow()
	if err != nil {
		return err
	}
	_, err = run("select-window", "-t", window, ";", "select-pane", "-t", pane.ID)
	return err
}

// titlePanes names both panes along their top edge, the focused one in clay,
// and fades the other, since tmux can't frame a pane and a divider alone
// doesn't show which side has the keyboard. Only
// the dashboard window gets this; session tabs stay one bare pane.
func titlePanes(window string) error {
	_, err := run(
		"set-option", "-w", "-t", window, "pane-border-status", "top", ";",
		"set-option", "-w", "-t", window, "pane-border-lines", "heavy", ";",
		// tmux colours only half of a divider shared by two panes by
		// default, which reads as a glitch; the title line marks focus.
		"set-option", "-w", "-t", window, "pane-border-indicators", "off", ";",
		"set-option", "-w", "-t", window, "pane-border-style", "fg=#3A3A52", ";",
		"set-option", "-w", "-t", window, "pane-active-border-style", "fg=#3A3A52", ";",
		"set-option", "-w", "-t", window, "pane-border-format", paneTitle, ";",
		// The side without focus fades to grey. Only default-coloured text
		// changes, so it works on light and dark terminals alike.
		"set-option", "-w", "-t", window, "window-style", "fg=#7A7A8C", ";",
		"set-option", "-w", "-t", window, "window-active-style", "fg=terminal,bg=terminal")
	return err
}

func untitlePanes() error {
	window, err := dashboardWindow()
	if err != nil {
		return err
	}
	_, err = run(
		"set-option", "-w", "-t", window, "pane-border-status", "off", ";",
		"set-option", "-wu", "-t", window, "window-style", ";",
		"set-option", "-wu", "-t", window, "window-active-style")
	return err
}

const paneTitle = "#{?pane_active,#[fg=#E8875B bold] ● ,#[fg=#6B6B7B] ○ }" +
	"#{?#{==:#{@deckk_pane},hq},deckk session,sessions} #[default]"

func CloseHQ() error {
	pane, open, err := HQ()
	if err != nil || !open {
		return err
	}
	_, err = run("kill-pane", "-t", pane.ID)
	return err
}
