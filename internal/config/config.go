package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	View       string `toml:"view"`
	Model      string `toml:"model"`
	Effort     string `toml:"effort"`
	Permission string `toml:"permission_mode"`
	ExtraArgs  string `toml:"extra_args"`
	Days       int    `toml:"days"`

	path string
}

type Option struct {
	Key     string
	Label   string
	Choices []string
	field   func(*Config) *string
}

// Options are what the settings screen cycles through. An empty choice means
// leave it to Claude's own settings. Modes that skip permission checks are
// left out on purpose; extra_args can still set them.
var Options = []Option{
	{"view", "View", []string{"", "default", "verbose", "focus"}, func(c *Config) *string { return &c.View }},
	{"model", "Model", []string{"", "opus", "sonnet", "fable", "haiku"}, func(c *Config) *string { return &c.Model }},
	{"effort", "Effort", []string{"", "low", "medium", "high", "xhigh", "max"}, func(c *Config) *string { return &c.Effort }},
	{"permission_mode", "Permissions", []string{"", "manual", "acceptEdits", "auto", "plan"}, func(c *Config) *string { return &c.Permission }},
}

func DefaultPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "deckk", "config.toml")
}

func Load(path string) (*Config, error) {
	c := &Config{Days: 30, path: path}
	_, err := toml.DecodeFile(path, c)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if c.Days <= 0 {
		c.Days = 30
	}
	return c, err
}

func (c *Config) Path() string { return c.path }

func (c *Config) Value(o Option) string { return *o.field(c) }

func (c *Config) Cycle(o Option) error {
	v := o.field(c)
	next := 0
	for i, choice := range o.Choices {
		if choice == *v {
			next = (i + 1) % len(o.Choices)
		}
	}
	*v = o.Choices[next]
	return c.Save()
}

func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# deckk settings. Empty values leave it to Claude's own settings.\n")
	b.WriteString("# extra_args is added as-is to every claude that deckk starts.\n\n")
	if err := toml.NewEncoder(&b).Encode(c); err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

func (c *Config) Claude(args ...string) string {
	parts := []string{"claude"}
	if c.View != "" {
		settings, _ := json.Marshal(map[string]string{"viewMode": c.View})
		parts = append(parts, "--settings", shellQuote(string(settings)))
	}
	for _, f := range [][2]string{{"--model", c.Model}, {"--effort", c.Effort}, {"--permission-mode", c.Permission}} {
		if f[1] != "" {
			parts = append(parts, f[0], shellQuote(f[1]))
		}
	}
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	if extra := strings.TrimSpace(c.ExtraArgs); extra != "" {
		parts = append(parts, extra)
	}
	return strings.Join(parts, " ")
}

var safe = regexp.MustCompile(`^[A-Za-z0-9._/:=-]+$`)

func shellQuote(s string) string {
	if safe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
