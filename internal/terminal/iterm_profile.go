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

	"github.com/theowecker/deckk/internal/config"
)

const (
	profileName = "deckk"
	profileGUID = "6F0D9C1E-5B7A-4D2E-9C3B-DECC0000C0DE"
)

// installProfile writes a dynamic profile that looks exactly like parent but
// sends Cmd+C to deckk. iTerm2 reloads dynamic profiles as soon as they change.
func installProfile(parent, tabKeys string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, "Library", "Application Support", "iTerm2", "DynamicProfiles")
	if _, err := os.Stat(filepath.Dir(dir)); err != nil {
		return err
	}
	data, err := profileJSON(parent, parentKeys(home, parent), tabKeys)
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
func profileJSON(parent string, keys map[string]any, tabKeys string) ([]byte, error) {
	keyMap := map[string]any{}
	maps.Copy(keyMap, keys)
	copyKey := map[string]any{"Action": 10, "Text": CopySequence[1:]}
	// Both spellings, since iTerm2 matches Cmd+C by either depending on
	// whether the event carried a key code.
	keyMap["0x63-0x100000"] = copyKey
	keyMap["0x63-0x100000-0x8"] = copyKey
	maps.Copy(keyMap, tabKeyMap(tabKeys))
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

// tabKeyMap sends the chosen Cmd keys to tmux as Ctrl+Shift+arrows. They
// take over iTerm2's own use of the keys, but only in the deckk tab.
func tabKeyMap(tabKeys string) map[string]any {
	prev := map[string]any{"Action": 10, "Text": "[1;6D"}
	next := map[string]any{"Action": 10, "Text": "[1;6C"}
	switch tabKeys {
	case config.TabKeysArrows:
		return map[string]any{"0xf702-0x320000": prev, "0xf703-0x320000": next}
	case config.TabKeysBrackets:
		// Both spellings, with and without the key code, as for Cmd+C.
		return map[string]any{"0x7b-0x120000": prev, "0x7b-0x120000-0x21": prev, "0x7d-0x120000": next, "0x7d-0x120000-0x1e": next}
	}
	return nil
}

// RefreshProfile rewrites an installed deckk profile for new tab keys.
// iTerm2 picks up the change in tabs that already use it.
func RefreshProfile(tabKeys string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(home, "Library", "Application Support", "iTerm2", "DynamicProfiles", "deckk.json"))
	if err != nil {
		return nil
	}
	var installed struct {
		Profiles []struct {
			Parent string `json:"Dynamic Profile Parent Name"`
		}
	}
	if json.Unmarshal(data, &installed) != nil || len(installed.Profiles) == 0 {
		return nil
	}
	return installProfile(installed.Profiles[0].Parent, tabKeys)
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
