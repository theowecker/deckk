package terminal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
)

const (
	profileName = "deckk"
	profileGUID = "6F0D9C1E-5B7A-4D2E-9C3B-DECC0000C0DE"
)

// installProfile writes a dynamic profile that looks exactly like parent but
// sends Cmd+C to deckk. iTerm2 reloads dynamic profiles as soon as they change.
func installProfile(parent string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, "Library", "Application Support", "iTerm2", "DynamicProfiles")
	if _, err := os.Stat(filepath.Dir(dir)); err != nil {
		return err
	}
	data, err := profileJSON(parent, parentKeys(home, parent))
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "deckk.json")
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, data) {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// A dynamic profile's Keyboard Map replaces its parent's instead of adding to
// it, so the parent's keys (Cmd+Backspace, Option+arrows) are copied in.
func profileJSON(parent string, keys map[string]any) ([]byte, error) {
	keyMap := map[string]any{}
	maps.Copy(keyMap, keys)
	copyKey := map[string]any{"Action": 10, "Text": CopySequence[1:]}
	// Both spellings, since iTerm2 matches Cmd+C by either depending on
	// whether the event carried a key code.
	keyMap["0x63-0x100000"] = copyKey
	keyMap["0x63-0x100000-0x8"] = copyKey
	profile := map[string]any{
		"Name":                        profileName,
		"Guid":                        profileGUID,
		"Dynamic Profile Parent Name": parent,
		"Keyboard Map":                keyMap,
		// tmux keeps each session's history. iTerm2's own scrollback only
		// holds the shell from before deckk started, and scrolling into it
		// hides the tab strip and swallows clicks.
		"Unlimited Scrollback":           false,
		"Scrollback Lines":               0,
		"Scrollback in Alternate Screen": false,
	}
	return json.MarshalIndent(map[string]any{"Profiles": []any{profile}}, "", "  ")
}

// parentKeys reads the parent profile's key mappings from iTerm2's
// preferences. A parent that can't be found just keeps iTerm2's defaults.
func parentKeys(home, parent string) map[string]any {
	prefs := filepath.Join(home, "Library", "Preferences", "com.googlecode.iterm2.plist")
	for i := 0; ; i++ {
		profile := fmt.Sprintf("New Bookmarks.%d", i)
		name, err := exec.Command("plutil", "-extract", profile+".Name", "raw", "-o", "-", prefs).Output()
		if err != nil {
			return nil
		}
		if strings.TrimSpace(string(name)) != parent {
			continue
		}
		out, err := exec.Command("plutil", "-extract", profile+".Keyboard Map", "json", "-o", "-", prefs).Output()
		if err != nil {
			return nil
		}
		var keys map[string]any
		if json.Unmarshal(out, &keys) != nil {
			return nil
		}
		return withoutDeprecated(keys, plistJSON(filepath.Join(appPath(home), "Contents", "Resources", "DeprecatedProfileKeyMappings.plist")))
	}
}

// iTerm2 asks to remove these from any dynamic profile that has them, every
// time the profile loads. They are old defaults it no longer needs.
func withoutDeprecated(keys, deprecated map[string]any) map[string]any {
	kept := map[string]any{}
	for key, value := range keys {
		if old, ok := deprecated[key]; !ok || !reflect.DeepEqual(old, value) {
			kept[key] = value
		}
	}
	return kept
}

func appPath(home string) string {
	for _, path := range []string{"/Applications/iTerm.app", filepath.Join(home, "Applications", "iTerm.app")} {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func plistJSON(path string) map[string]any {
	out, err := exec.Command("plutil", "-convert", "json", "-o", "-", path).Output()
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(out, &m) != nil {
		return nil
	}
	return m
}

// useProfile switches the current iTerm2 tab, and only that tab, to a profile.
func useProfile(w io.Writer, name string) {
	fmt.Fprintf(w, "\x1b]1337;SetProfile=%s\x07", name)
}
