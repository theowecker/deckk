package tmux

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const Session = "deckk"

// DECKK_SOCKET runs a second deckk beside the real one, which development and
// tests rely on.
var Socket = cmp.Or(os.Getenv("DECKK_SOCKET"), "deckk")

var ErrMissing = errors.New("deckk needs tmux: brew install tmux")

type Window struct {
	ID        string
	TTY       string
	Dashboard bool
	// Popped-out windows live in a session of their own; ClientTTY is the
	// iTerm2 tab attached to it.
	Session   string
	ClientTTY string
	Active    bool
	InMode    bool
	Width     int
}

func (w Window) PoppedOut() bool { return w.Session != Session }

func Path() (string, error) {
	path, err := exec.LookPath("tmux")
	if err != nil {
		return "", ErrMissing
	}
	return path, nil
}

func Inside() bool {
	socket, _, _ := strings.Cut(os.Getenv("TMUX"), ",")
	return strings.HasSuffix(socket, "/"+Socket)
}

func run(args ...string) (string, error) {
	cmd := exec.Command("tmux", append([]string{"-L", Socket}, args...)...)
	out, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("tmux %s: %s", command(args), result)
	}
	return result, nil
}

func command(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

func HasSession() bool {
	_, err := run("has-session", "-t", "="+Session)
	return err == nil
}

func Start(config, dashboard string) error {
	if _, err := run("-f", config, "new-session", "-d", "-s", Session, dashboard); err != nil {
		return err
	}
	if err := markPane("="+Session+":0.0", dashboardPane); err != nil {
		return err
	}
	return markDashboard("=" + Session + ":0")
}

func Reload(config string) error {
	_, err := run("source-file", config)
	return err
}

// Restarting on every launch is what makes upgrades take effect. Only the
// dashboard pane restarts, so the deckk session beside it keeps running.
func RestartDashboard(dashboard string) error {
	if pane, found, err := findPane(dashboardPane); err == nil && found {
		if _, err := run("respawn-pane", "-k", "-t", pane.ID, dashboard); err != nil {
			return err
		}
		_ = leaveModes(pane.ID)
		return markPane(pane.ID, dashboardPane)
	}
	id, err := run("new-window", "-d", "-b", "-P", "-F", "#{window_id}", "-t", "="+Session+":{start}", dashboard)
	if err != nil {
		return err
	}
	if err := markPane(id, dashboardPane); err != nil {
		return err
	}
	return markDashboard(id)
}

func markDashboard(target string) error {
	_, err := run("set-option", "-w", "-t", target, "@deckk_dashboard", "1")
	return err
}

func Windows() ([]Window, error) {
	out, err := run("list-windows", "-a", "-F", "#{window_id}\t#{pane_tty}\t#{@deckk_dashboard}\t#{session_name}\t#{&&:#{window_active},#{session_attached}}\t#{pane_in_mode}\t#{window_width}")
	if err != nil {
		return nil, err
	}
	clients := map[string]string{}
	if list, err := run("list-clients", "-F", "#{session_name}\t#{client_tty}"); err == nil {
		for line := range strings.Lines(list) {
			if session, tty, ok := strings.Cut(strings.TrimRight(line, "\n"), "\t"); ok {
				clients[session] = tty
			}
		}
	}
	var windows []Window
	for line := range strings.Lines(out) {
		fields := strings.Split(strings.TrimRight(line, "\n"), "\t")
		if len(fields) != 7 {
			continue
		}
		windows = append(windows, Window{
			ID: fields[0], TTY: fields[1], Dashboard: fields[2] == "1",
			Session: fields[3], ClientTTY: clients[fields[3]], Active: fields[4] == "1",
			InMode: fields[5] != "0", Width: atoi(fields[6]),
		})
	}
	return windows, nil
}

func PopOut(windowID string) (string, error) {
	session := Session + "-" + strings.TrimPrefix(windowID, "@")
	placeholder, err := run("new-session", "-d", "-P", "-F", "#{window_id}", "-s", session)
	if err != nil {
		return "", err
	}
	_, err = run(
		"move-window", "-s", windowID, "-t", "="+session+":", ";",
		"kill-window", "-t", placeholder, ";",
		"set-option", "-t", "="+session+":", "status", "off", ";",
		"set-option", "-t", "="+session+":", "set-titles-string", "#{?@deckk_name,#{@deckk_name},deckk}")
	if err != nil {
		_, _ = run("kill-session", "-t", "="+session)
		return "", err
	}
	return session, nil
}

// The emptied session ends, which closes the iTerm2 tab attached to it.
func BringBack(windowID string) error {
	_, err := run("move-window", "-s", windowID, "-t", "="+Session+":", ";", "select-window", "-t", windowID)
	return err
}

func MoveAfter(windowID, afterID string) error {
	_, err := run("move-window", "-a", "-s", windowID, "-t", afterID, ";", "move-window", "-r", "-t", "="+Session+":")
	return err
}

func AttachCommand(tmuxPath, session string) string {
	return "exec " + shellQuote(tmuxPath) + " -L " + Socket + " attach-session -t " + shellQuote("="+session)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// The name goes in a user option, not -n, because tmux expands formats in
// window names and titles come from transcript text.
func Open(dir, name, command string, focus bool) error {
	args := []string{"new-window", "-P", "-F", "#{window_id}", "-t", "=" + Session + ":", "-c", dir}
	if !focus {
		args = append(args, "-d")
	}
	id, err := run(append(args, command)...)
	if err != nil {
		return err
	}
	_, err = run("set-option", "-w", "-t", id, "@deckk_name", name)
	return err
}

func Select(windowID string) error {
	_, err := run("select-window", "-t", windowID)
	return err
}

func Close(windowID string) error {
	_, err := run("kill-window", "-t", windowID)
	return err
}

func Detach() error {
	_, err := run("detach-client", "-s", "="+Session)
	return err
}

func Kill() error {
	if !HasSession() {
		return nil
	}
	_, err := run("kill-server")
	return err
}

type Label struct {
	Name   string
	Status string
	Reel   string
	Info   string
}

func SetLabels(labels map[string]Label) error {
	if len(labels) == 0 {
		return nil
	}
	var args []string
	for id, l := range labels {
		if len(args) > 0 {
			args = append(args, ";")
		}
		args = append(args,
			"set-option", "-w", "-t", id, "@deckk_name", l.Name, ";",
			"set-option", "-w", "-t", id, "@deckk_cap", Cap(l.Name), ";",
			"set-option", "-w", "-t", id, "@deckk_status", l.Status, ";",
			"set-option", "-w", "-t", id, "@deckk_reel", l.Reel, ";",
			"set-option", "-w", "-t", id, "@deckk_info", l.Info)
	}
	_, err := run(args...)
	return err
}

// Attach runs as a child rather than replacing deckk, so deckk can tidy up
// the terminal after you leave.
func Attach(tmuxPath string) error {
	cmd := exec.Command(tmuxPath, "-L", Socket, "attach-session", "-t", "="+Session)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "TMUX=") })
	return cmd.Run()
}

type Server struct{}

// Something iTerm2 sends on attach can leave the dashboard in an empty
// view-mode, which only Escape gets out of. It never needs a mode, so leave it.
func (Server) Windows() ([]Window, error) {
	windows, err := Windows()
	_ = PlaceTabs()
	panes, _ := DashboardPanes()
	for _, p := range panes {
		if p.Role == dashboardPane && p.InMode {
			_ = leaveModes(p.ID)
		}
	}
	// The deckk session can exit on its own, leaving pane titles over a
	// dashboard that is alone again.
	if len(panes) == 1 {
		_ = untitlePanes()
	}
	return windows, err
}

func leaveModes(target string) error {
	_, err := run("copy-mode", "-q", "-t", target)
	return err
}
func (Server) Open(dir, name, command string, focus bool) error {
	return Open(dir, name, command, focus)
}
func (Server) Select(windowID string) error            { return Select(windowID) }
func (Server) Close(windowID string) error             { return Close(windowID) }
func (Server) SetLabels(labels map[string]Label) error { return SetLabels(labels) }
func (Server) Detach() error                           { return Detach() }

// The strip uses these to tell which tab a dragged one is over. Status-left is
// one cell and tabs are one cell apart.
func PlaceTabs() error {
	out, err := run("list-windows", "-t", "="+Session, "-F", "#{window_id}\t#{@deckk_cap}\t#{@deckk_x}:#{@deckk_x2}")
	if err != nil {
		return err
	}
	var args []string
	x := 1
	for line := range strings.Lines(out) {
		fields := strings.Split(strings.TrimRight(line, "\n"), "\t")
		if len(fields) != 3 {
			continue
		}
		end := x + ansi.StringWidth(fields[1])
		if fields[2] != fmt.Sprintf("%d:%d", x, end) {
			if len(args) > 0 {
				args = append(args, ";")
			}
			args = append(args,
				"set-option", "-w", "-t", fields[0], "@deckk_x", strconv.Itoa(x), ";",
				"set-option", "-w", "-t", fields[0], "@deckk_x2", strconv.Itoa(end))
		}
		x = end + 1
	}
	if len(args) == 0 {
		return nil
	}
	_, err = run(args...)
	return err
}

func SetBackground(hex string) error {
	_, err := run("set-option", "-g", "@deckk_bg", hex)
	return err
}
