// Package terminal drives the terminal app deckk runs in: opening tabs for
// sessions moved out of deckk, and finding or closing the tab a Claude
// session runs in.
package terminal

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// CopySequence is what Cmd+C sends where deckk can remap it. tmux binds it as
// a user key and copies its own selection. Any CSI that starts like a real key
// confuses tmux's parser and the tail leaks into the pane; nothing uses 1337.
const CopySequence = "\x1b[1337~"

var (
	ErrNotFound    = errors.New("no terminal tab has that tty")
	ErrUnsupported = errors.New("deckk can't open tabs in this terminal")
)

// App is a scriptable terminal. Scripts are AppleScript run inside a tell
// block. eachTab loops over every tab and runs {action} on the one whose
// tty is {tty}, with w, t (and s, where tabs hold sessions) in scope.
type App struct {
	Name    string
	program string // $TERM_PROGRAM inside it
	process string
	bundle  string
	eachTab string
	focus   string
	close   string
	open    string // {command} is the shell command for the new tab
	// setup runs before deckk attaches and returns what undoes it.
	setup func(out io.Writer) (undo func())
}

func Lookup(program string) (App, bool) {
	for _, app := range apps {
		if app.program == program {
			return app, true
		}
	}
	return App{}, false
}

// Current is the terminal deckk was started from.
func Current() (App, bool) { return Lookup(os.Getenv("TERM_PROGRAM")) }

func (a App) Program() string { return a.program }

// Setup prepares the tab deckk is about to take over.
func (a App) Setup(out io.Writer) (undo func()) {
	if a.setup == nil {
		return func() {}
	}
	return a.setup(out)
}

func (a App) CanOpenTabs() bool { return a.open != "" }

func (a App) OpenTab(command string) error {
	if !a.CanOpenTabs() {
		return ErrUnsupported
	}
	_, err := a.run(strings.ReplaceAll(a.open, "{command}", quote(command)))
	return err
}

// Focus brings forward the tab on tty in whichever supported terminal has it.
func Focus(tty string) error { return onTab(tty, func(a App) string { return a.focus }) }

// Close shuts the tab on tty. Only call once Claude has quit, or the
// terminal asks before closing a busy tab.
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
	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	result := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("%s: %s", a.Name, result)
	}
	return result, nil
}

func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
