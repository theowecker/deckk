package config

import (
	"path/filepath"
	"testing"
)

func TestClaudeCommandCarriesSettings(t *testing.T) {
	c := &Config{View: "focus", Model: "opus", Permission: "plan", ExtraArgs: "--add-dir ~/shared"}
	want := `claude --settings '{"viewMode":"focus"}' --model opus --permission-mode plan --resume abc --add-dir ~/shared`
	if got := c.Claude("--resume", "abc"); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := (&Config{}).Claude(); got != "claude" {
		t.Errorf("empty config should run plain claude, got %s", got)
	}
}

func TestCycleWrapsAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	view := Options[0]
	for range len(view.Choices) - 1 {
		if err := c.Cycle(view); err != nil {
			t.Fatal(err)
		}
	}
	if c.View != "focus" {
		t.Fatalf("view %q", c.View)
	}
	if err := c.Cycle(view); err != nil {
		t.Fatal(err)
	}
	if c.View != "" {
		t.Fatalf("cycling past the end should wrap to Claude's default, got %q", c.View)
	}

	c.Model = "sonnet"
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Model != "sonnet" || reloaded.Days != 30 {
		t.Fatalf("reloaded %+v", reloaded)
	}
}
