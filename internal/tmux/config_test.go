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
}
