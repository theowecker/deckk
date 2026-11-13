package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func editLine(at time.Time, tool, file string) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"content":[{"type":"tool_use","name":%q,"input":{"file_path":%q}}]}}`+"\n", at.Format(time.RFC3339Nano), tool, file)
}

func TestEditedFilesFollowsAGrowingTranscript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	start := time.Now()
	write := func(s string) {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.WriteString(s); err != nil {
			t.Fatal(err)
		}
	}
	check := func(want ...string) {
		t.Helper()
		got, err := EditedFiles(path, start)
		if err != nil || !slices.Equal(got, want) {
			t.Fatalf("got %v %v, want %v", got, err, want)
		}
	}
	write(editLine(start.Add(-time.Hour), "Edit", "/old.go"))
	write(editLine(start.Add(time.Second), "Edit", "/a.go"))
	write(`{"type":"assistant","timestamp":"x","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/read.go"}}]}}` + "\n")
	check("/a.go")

	line := editLine(start.Add(2*time.Second), "Write", "/b.go")
	write(line[:20])
	check("/a.go")
	write(line[20:])
	check("/b.go", "/a.go")

	write(editLine(start.Add(3*time.Second), "MultiEdit", "/a.go"))
	check("/a.go", "/b.go")
}
