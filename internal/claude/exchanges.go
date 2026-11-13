package claude

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

type Exchange struct {
	Prompt string
	Reply  string
}

// TranscriptPath finds a session's transcript under any project folder.
func TranscriptPath(home, id string) string {
	paths, _ := filepath.Glob(filepath.Join(home, "projects", "*", id+".jsonl"))
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

// Exchanges returns the last n prompts in a transcript, each with the final
// text Claude replied with. Only the tail is read, so long sessions stay cheap.
func Exchanges(path string, n int) ([]Exchange, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	chunk, whole, err := readTail(f, info.Size(), 2<<20)
	if err != nil {
		return nil, err
	}
	lines := bytes.Split(chunk, []byte{'\n'})
	if !whole && len(lines) > 0 {
		lines = lines[1:]
	}
	var turns []Exchange
	for _, line := range lines {
		var e entry
		if len(line) == 0 || json.Unmarshal(line, &e) != nil || e.IsSidechain || e.IsMeta {
			continue
		}
		switch e.Type {
		case "user":
			if prompt := promptText(e.Message); prompt != "" {
				turns = append(turns, Exchange{Prompt: prompt})
			}
		case "assistant":
			if reply := replyText(e.Message); reply != "" && len(turns) > 0 {
				turns[len(turns)-1].Reply = reply
			}
		}
	}
	return turns[max(len(turns)-n, 0):], nil
}
