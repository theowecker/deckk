package tmux

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// tmux shows config errors in view-mode over the active pane on attach,
// which looks like a blank, frozen dashboard.
func TestConfigLoadsCleanly(t *testing.T) {
	bin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux not installed")
	}
	dir := t.TempDir()
	conf := filepath.Join(dir, "tmux.conf")
	if err := os.WriteFile(conf, []byte(tmuxConf), 0o644); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "sock")
	tmux := func(args ...string) (string, error) {
		out, err := exec.Command(bin, append([]string{"-S", socket}, args...)...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	if out, err := tmux("-f", "/dev/null", "new-session", "-d"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	t.Cleanup(func() { _, _ = tmux("kill-server") })
	if out, err := tmux("source-file", conf); err != nil || out != "" {
		t.Fatalf("config errors: %v\n%s", err, out)
	}
	for _, binding := range []struct{ table, key, does string }{
		{"root", "C-S-f", "search-backward-text"},
		{"root", "C-S-F", "search-backward-text"},
		{"root", "C-S-g", "search-again"},
		{"root", "C-S-G", "search-again"},
		{"copy-mode", "C-S-f", "search-backward-text"},
		{"copy-mode-vi", "C-S-g", "search-again"},
		{"copy-mode", "C-S-Right", "send-keys -K"},
		{"copy-mode", "C-Space", "send-keys -K"},
		{"copy-mode", "Any", "send-keys -K"},
		{"copy-mode", "S-MouseDown1Pane", "jump-to-mark"},
		{"root", "DoubleClick1Pane", "@deckk_click"},
		{"root", "C-S-Enter", "@deckk_dash"},
	} {
		out, err := tmux("list-keys", "-T", binding.table, binding.key)
		if err != nil || !strings.Contains(out, binding.does) {
			t.Errorf("%s %s should %s, got %q (%v)", binding.table, binding.key, binding.does, out, err)
		}
	}
}
