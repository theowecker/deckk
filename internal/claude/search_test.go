package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMentioningNeedsEveryWordAnywhereInTheTranscript(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	both := write("a.jsonl", `{"message":"we moved the Webhook retries"}`+"\n"+`{"message":"into a queue"}`)
	one := write("b.jsonl", `{"message":"webhook only"}`)
	found, err := Mentioning([]string{both, one}, "webhook queue")
	if err != nil {
		t.Fatal(err)
	}
	if !found[both] || found[one] || len(found) != 1 {
		t.Fatalf("found %v", found)
	}
	if found, err := Mentioning([]string{both}, "absent"); err != nil || len(found) != 0 {
		t.Fatalf("found %v, err %v", found, err)
	}
}
