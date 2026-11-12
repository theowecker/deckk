package terminal

import (
	"strings"
	"testing"
)

func TestAppsAreComplete(t *testing.T) {
	for _, app := range apps {
		if app.Name == "" || app.program == "" || app.process == "" || app.bundle == "" || app.focus == "" || app.close == "" {
			t.Errorf("%+v is missing a field", app)
		}
		for _, placeholder := range []string{"{tty}", "{action}"} {
			if !strings.Contains(app.eachTab, placeholder) {
				t.Errorf("%s eachTab lacks %s", app.Name, placeholder)
			}
		}
		if !strings.Contains(app.open, "{command}") {
			t.Errorf("%s open lacks {command}", app.Name)
		}
		if found, ok := Lookup(app.program); !ok || found.Name != app.Name {
			t.Errorf("Lookup(%q) = %v, %v", app.program, found.Name, ok)
		}
	}
}

func TestUnknownTerminalCantOpenTabs(t *testing.T) {
	app, ok := Lookup("WarpTerminal")
	if ok || app.CanOpenTabs() || app.OpenTab("true") != ErrUnsupported {
		t.Fatal("an unknown terminal should refuse to open tabs")
	}
}
