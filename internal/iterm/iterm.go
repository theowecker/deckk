package iterm

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var ErrNotFound = errors.New("no iTerm2 session has that tty")

func Focus(tty string) error {
	out, err := run(fmt.Sprintf(`
if application id "com.googlecode.iterm2" is not running then return "missing"
tell application id "com.googlecode.iterm2"
	repeat with w in windows
		repeat with t in tabs of w
			repeat with s in sessions of t
				if tty of s is %s then
					select w
					tell t to select
					tell s to select
					activate
					return "ok"
				end if
			end repeat
		end repeat
	end repeat
	return "missing"
end tell`, quote(tty)))
	if err != nil {
		return err
	}
	if out != "ok" {
		return ErrNotFound
	}
	return nil
}

func OpenTab(command string) error {
	_, err := run(fmt.Sprintf(`
tell application id "com.googlecode.iterm2"
	activate
	if (count of windows) is 0 then
		create window with default profile
	else
		tell current window to create tab with default profile
	end if
	tell current session of current window to write text %s
end tell`, quote(command)))
	return err
}

// Only call once Claude has quit, or iTerm2 asks before closing a busy tab.
func Close(tty string) error {
	_, err := run(fmt.Sprintf(`
if application id "com.googlecode.iterm2" is not running then return "missing"
tell application id "com.googlecode.iterm2"
	repeat with w in windows
		repeat with t in tabs of w
			repeat with s in sessions of t
				if tty of s is %s then
					close s
					return "ok"
				end if
			end repeat
		end repeat
	end repeat
	return "missing"
end tell`, quote(tty)))
	return err
}

func run(script string) (string, error) {
	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	result := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("osascript: %s", result)
	}
	return result, nil
}

func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
