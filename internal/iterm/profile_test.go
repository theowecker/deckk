package iterm

import (
	"encoding/json"
	"testing"
)

func TestProfileInheritsAndRemapsCmdC(t *testing.T) {
	data, err := profileJSON("Default", map[string]any{"0x7f-0x100000": map[string]any{"Action": 11, "Text": "0x15"}})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Profiles []struct {
			Name        string                    `json:"Name"`
			Guid        string                    `json:"Guid"`
			Parent      string                    `json:"Dynamic Profile Parent Name"`
			KeyboardMap map[string]map[string]any `json:"Keyboard Map"`
			Scrollback  float64                   `json:"Scrollback Lines"`
			Unlimited   bool                      `json:"Unlimited Scrollback"`
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
	if key["Action"] != float64(10) || key["Text"] != CopySequence[1:] {
		t.Fatalf("Cmd+C should send the deckk copy sequence, got %v", key)
	}
	if p.Scrollback != 0 || p.Unlimited {
		t.Fatalf("iTerm2 should keep no scrollback of its own: %+v", p)
	}
	if p.KeyboardMap["0x7f-0x100000"]["Text"] != "0x15" {
		t.Fatalf("the parent's Cmd+Backspace mapping was dropped: %v", p.KeyboardMap)
	}
}

func TestDropsDeprecatedMappings(t *testing.T) {
	ctrlLeft := map[string]any{"Action": float64(10), "Text": "[1;5D"}
	deprecated := map[string]any{"0xf702-0x240000": ctrlLeft, "0xf703-0x240000": ctrlLeft}
	keys := map[string]any{
		"0xf702-0x240000": ctrlLeft,
		"0xf703-0x240000": map[string]any{"Action": float64(11), "Text": "0x5"},
		"0x7f-0x100000":   map[string]any{"Action": float64(11), "Text": "0x15"},
	}
	kept := withoutDeprecated(keys, deprecated)
	if _, ok := kept["0xf702-0x240000"]; ok || len(kept) != 2 {
		t.Fatalf("only exact deprecated mappings should go, kept %v", kept)
	}
}
