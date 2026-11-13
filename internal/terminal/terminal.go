package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// A terminal waiting on a macOS automation prompt holds an AppleScript call
// for two minutes. Long enough to click Allow, short enough not to stall.
const scriptTimeout = 15 * time.Second

// tmux binds this as a user key. Any CSI that starts like a real key confuses
// tmux's parser and the tail leaks into the pane; nothing uses 1337.
const CopySequence = "\x1b[1337~"

// FindSequence and FindNextSequence are what Cmd+F and Cmd+G send, for tmux
// to search the whole history; iTerm2's own find only sees the screen.
const (
	FindSequence     = "\x1b[1338~"
	FindNextSequence = "\x1b[1339~"
)

var (
	ErrNotFound    = errors.New("no terminal tab has that tty")
	ErrUnsupported = errors.New("deckk can't open tabs in this terminal")
)

// Scripts are AppleScript run inside a tell block. eachTab runs {action} on the
// tab whose tty is {tty}, with w, t (and s, where tabs hold sessions) in scope.
type App struct {
	Name    string
	program string // $TERM_PROGRAM inside it
	process string
	bundle  string
	eachTab string
	focus   string
	close   string
	open    string // {command} is the shell command for the new tab
	setup   func(out io.Writer, tabKeys string) (undo func())
}

func Lookup(program string) (App, bool) {
	for _, app := range apps {
		if app.program == program {
			return app, true
		}
	}
	return App{}, false
}

func Current() (App, bool) { return Lookup(os.Getenv("TERM_PROGRAM")) }

func (a App) Program() string { return a.program }

func (a App) Setup(out io.Writer, tabKeys string) (undo func()) {
	if a.setup == nil {
		return func() {}
	}
	return a.setup(out, tabKeys)
}

func (a App) CanOpenTabs() bool { return a.open != "" }

func (a App) OpenTab(command string) error {
	if !a.CanOpenTabs() {
		return ErrUnsupported
	}
	_, err := a.run(strings.ReplaceAll(a.open, "{command}", quote(command)))
	return err
}

func Focus(tty string) error { return onTab(tty, func(a App) string { return a.focus }) }

// Only call once Claude has quit, or the terminal asks before closing a busy
// tab.
func Close(tty string) error { return onTab(tty, func(a App) string { return a.close }) }

func onTab(tty string, action func(App) string) error {
	var firstErr error
	for _, app := range apps {
		if !app.running() {
			continue
		}
		script := strings.NewReplacer("{tty}", quote(tty), "{action}", action(app)).Replace(app.eachTab)
		out, err := app.run(script + "\nreturn \"missing\"")
		if err != nil {
			firstErr = orErr(firstErr, err)
			continue
		}
		if out == "ok" {
			return nil
		}
	}
	return orErr(firstErr, ErrNotFound)
}

func orErr(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}

// pgrep rather than AppleScript, which fails to compile a tell block for an
// app that isn't installed.
func (a App) running() bool {
	return exec.Command("pgrep", "-xq", a.process).Run() == nil
}

func (a App) run(body string) (string, error) {
	script := fmt.Sprintf("tell application id %s\n%s\nend tell", quote(a.bundle), body)
	ctx, cancel := context.WithTimeout(context.Background(), scriptTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "osascript", "-e", script).CombinedOutput()
	result := strings.TrimSpace(string(out))
	if ctx.Err() != nil {
		return "", fmt.Errorf("%s didn't answer. Check System Settings > Privacy & Security > Automation", a.Name)
	}
	if err != nil {
		return "", fmt.Errorf("%s: %s", a.Name, result)
	}
	return result, nil
}

func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

type Notification struct {
	Title, Subtitle, Body string
	// Group replaces an earlier notification with the same group.
	Group string
	// OnClick is a shell command run when the notification is clicked.
	OnClick string
}

// terminal-notifier can run a command on click; a notification from osascript
// opens Script Editor instead, so it is only the fallback.
func (a App) Notify(n Notification) error {
	if path, err := exec.LookPath("terminal-notifier"); err == nil {
		args := []string{"-title", n.Title, "-subtitle", n.Subtitle, "-message", n.Body, "-group", n.Group}
		if n.OnClick != "" {
			args = append(args, "-execute", n.OnClick)
		}
		if a.bundle != "" {
			args = append(args, "-activate", a.bundle)
		}
		return exec.Command(path, args...).Run()
	}
	// The text goes to AppleScript as arguments rather than inside the script,
	// so quotes in it can't break out.
	script := `on run argv
	display notification (item 3 of argv) with title (item 1 of argv) subtitle (item 2 of argv)
end run`
	return exec.Command("osascript", "-e", script, n.Title, n.Subtitle, n.Body).Run()
}
