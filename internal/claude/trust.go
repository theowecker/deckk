package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Trusted reports whether the user has told Claude Code it may work in dir.
// Claude asks once per folder and keeps the answer in its own settings,
// which deckk only reads.
func Trusted(dir string) bool {
	path := filepath.Join(os.Getenv("HOME"), ".claude.json")
	if custom := os.Getenv("CLAUDE_CONFIG_DIR"); custom != "" {
		path = filepath.Join(custom, ".claude.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var settings struct {
		Projects map[string]struct {
			Trusted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	if json.Unmarshal(data, &settings) != nil {
		return false
	}
	return settings.Projects[filepath.Clean(dir)].Trusted
}
