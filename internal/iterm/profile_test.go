package iterm

import (
	"encoding/json"
	"testing"
)

func TestProfileInheritsAndRemapsCmdC(t *testing.T) {
	data, err := profileJSON("Default")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Profiles []struct {
			Name        string                    `json:"Name"`
			Guid        string                    `json:"Guid"`
			Parent      string                    `json:"Dynamic Profile Parent Name"`
			KeyboardMap map[string]map[string]any `json:"Keyboard Map"`
		}
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	p := doc.Profiles[0]
	if p.Name != ProfileName || p.Guid == "" || p.Parent != "Default" {
		t.Fatalf("profile %+v", p)
	}
	key := p.KeyboardMap["0x63-0x100000"]
	if key["Action"] != float64(10) || key["Text"] != "[deckk-copy~" {
		t.Fatalf("Cmd+C should send the deckk copy sequence, got %v", key)
	}
}
