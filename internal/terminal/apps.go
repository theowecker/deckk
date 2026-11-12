package terminal

import (
	"fmt"
	"io"
	"os"
)

var apps = []App{iTerm, ghostty, appleTerminal}

var iTerm = App{
	Name:    "iTerm2",
	program: "iTerm.app",
	process: "iTerm2",
	bundle:  "com.googlecode.iterm2",
	eachTab: eachSession("sessions"),
	focus:   "select w\ntell t to select\ntell s to select\nactivate",
	close:   "close s",
	open: `activate
if (count of windows) is 0 then
	create window with default profile
else
	tell current window to create tab with default profile
end if
tell current session of current window to write text {command}`,
	setup: useDeckkProfile,
}

// Ghostty is scriptable from 1.3.
var ghostty = App{
	Name:    "Ghostty",
	program: "ghostty",
	process: "ghostty",
	bundle:  "com.mitchellh.ghostty",
	eachTab: eachSession("terminals"),
	focus:   "focus s\nactivate",
	close:   "close s",
	open: `activate
set cfg to new surface configuration
set initial input of cfg to {command} & linefeed
if (count of windows) is 0 then
	new window with configuration cfg
else
	new tab in front window with configuration cfg
end if`,
}

// Terminal can't script a new tab without accessibility access, so sessions
// moved out get a window. It also can't close one tab of several.
var appleTerminal = App{
	Name:    "Terminal",
	program: "Apple_Terminal",
	process: "Terminal",
	bundle:  "com.apple.Terminal",
	eachTab: `repeat with w in windows
	repeat with t in tabs of w
		if tty of t is {tty} then
			{action}
			return "ok"
		end if
	end repeat
end repeat`,
	focus: "set selected of t to true\nset index of w to 1\nactivate",
	close: "if (count of tabs of w) is 1 then close w",
	open:  "activate\ndo script {command}",
}

func eachSession(sessions string) string {
	return fmt.Sprintf(`repeat with w in windows
	repeat with t in tabs of w
		repeat with s in %s of t
			if tty of s is {tty} then
				{action}
				return "ok"
			end if
		end repeat
	end repeat
end repeat`, sessions)
}

// The deckk profile sends Cmd+C to deckk, and the tab switches back to the
// profile it started in when deckk quits.
func useDeckkProfile(out io.Writer) func() {
	parent := os.Getenv("ITERM_PROFILE")
	if parent == "" || parent == profileName || installProfile(parent) != nil {
		return func() {}
	}
	useProfile(out, profileName)
	return func() { useProfile(out, parent) }
}
