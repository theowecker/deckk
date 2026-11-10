package iterm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	ProfileName = "deckk"
	profileGUID = "6F0D9C1E-5B7A-4D2E-9C3B-DECC0000C0DE"
	// CopySequence is what Cmd+C sends inside deckk. tmux binds it as a user
	// key, so iTerm2 never runs its own copy and never nags about it.
	// Any CSI that starts like a real key confuses tmux's parser, and the tail
	// leaks into the pane. Nothing uses 1337.
	CopySequence = "\x1b[1337~"
)

// InstallProfile writes a dynamic profile that looks exactly like parent but
// sends Cmd+C to deckk. iTerm2 reloads dynamic profiles as soon as they change.
func InstallProfile(parent string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, "Library", "Application Support", "iTerm2", "DynamicProfiles")
	if _, err := os.Stat(filepath.Dir(dir)); err != nil {
		return err
	}
	data, err := profileJSON(parent)
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

func profileJSON(parent string) ([]byte, error) {
	copyKey := map[string]any{"Action": 10, "Text": CopySequence[1:]}
	profile := map[string]any{
		"Name":                        ProfileName,
		"Guid":                        profileGUID,
		"Dynamic Profile Parent Name": parent,
		// Both spellings, since iTerm2 matches Cmd+C by either depending on
		// whether the event carried a key code.
		"Keyboard Map": map[string]any{
			"0x63-0x100000":     copyKey,
			"0x63-0x100000-0x8": copyKey,
		},
	}
	return json.MarshalIndent(map[string]any{"Profiles": []any{profile}}, "", "  ")
}

// UseProfile switches the current iTerm2 tab, and only that tab, to a profile.
func UseProfile(w io.Writer, name string) {
	fmt.Fprintf(w, "\x1b]1337;SetProfile=%s\x07", name)
}
