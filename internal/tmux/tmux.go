package tmux

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
)

const (
	Socket  = "deckk"
	Session = "deckk"
)

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
	return markDashboard("=" + Session + ":0")
}

func Reload(config string) error {
	_, err := run("source-file", config)
	return err
}

// Restarting on every launch is what makes upgrades take effect.
func RestartDashboard(dashboard string) error {
	windows, err := Windows()
	if err != nil {
		return err
	}
	for _, w := range windows {
		if w.Dashboard {
			_, err := run("respawn-window", "-k", "-t", w.ID, dashboard)
			return err
		}
	}
	id, err := run("new-window", "-d", "-b", "-P", "-F", "#{window_id}", "-t", "="+Session+":{start}", dashboard)
	if err != nil {
		return err
	}
	return markDashboard(id)
}

func markDashboard(target string) error {
	_, err := run("set-option", "-w", "-t", target, "@deckk_dashboard", "1")
	return err
}

func Windows() ([]Window, error) {
	out, err := run("list-windows", "-a", "-F", "#{window_id}\t#{pane_tty}\t#{@deckk_dashboard}\t#{session_name}\t#{&&:#{window_active},#{session_attached}}")
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
		if len(fields) != 5 {
			continue
		}
		windows = append(windows, Window{
			ID: fields[0], TTY: fields[1], Dashboard: fields[2] == "1",
			Session: fields[3], ClientTTY: clients[fields[3]], Active: fields[4] == "1",
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
			"set-option", "-w", "-t", id, "@deckk_status", l.Status, ";",
			"set-option", "-w", "-t", id, "@deckk_reel", l.Reel)
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

func (Server) Windows() ([]Window, error) { return Windows() }
func (Server) Open(dir, name, command string, focus bool) error {
	return Open(dir, name, command, focus)
}
func (Server) Select(windowID string) error            { return Select(windowID) }
func (Server) Close(windowID string) error             { return Close(windowID) }
func (Server) SetLabels(labels map[string]Label) error { return SetLabels(labels) }
func (Server) Detach() error                           { return Detach() }
