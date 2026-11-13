package claude

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"
	"time"
)

type Exchange struct {
	Prompt string
	Reply  string
}

// ProjectDirs are where Claude may keep transcripts for sessions started in
// cwd: the path with separators and dots turned into dashes. Older versions
// turned underscores into dashes too.
func ProjectDirs(home, cwd string) []string {
	current := filepath.Join(home, "projects", unsafeName.ReplaceAllString(cwd, "-"))
	older := filepath.Join(home, "projects", notAlphanumeric.ReplaceAllString(cwd, "-"))
	if current == older {
		return []string{current}
	}
	return []string{current, older}
}

var (
	unsafeName      = regexp.MustCompile(`[^A-Za-z0-9_-]`)
	notAlphanumeric = regexp.MustCompile(`[^A-Za-z0-9-]`)
)

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

// Tools whose input names the file they change. Edits made through shell
// commands can't be seen this way.
var editTools = map[string]string{"Edit": "file_path", "MultiEdit": "file_path", "Write": "file_path", "NotebookEdit": "notebook_path"}

var edits = struct {
	sync.Mutex
	byPath map[string]editsEntry
}{byPath: map[string]editsEntry{}}

type editsEntry struct {
	size  int64
	since time.Time
	files []string
}

// Sessions without a start time count edits from this long ago.
const editsFor = 6 * time.Hour

// EditsOf reads which files each running session has edited since it
// started, by session ID.
func EditsOf(sessions []Session) map[string][]string {
	edits := map[string][]string{}
	for _, s := range sessions {
		if !s.Running() || s.Path == "" {
			continue
		}
		since := s.StartedAt
		if since.IsZero() {
			since = time.Now().Add(-editsFor)
		}
		if files, err := EditedFiles(s.Path, since); err == nil {
			edits[s.ID] = files
		}
	}
	return edits
}

// EditedFiles lists the files a session changed since a time, newest first,
// counting its subagents' edits too. After the first read it only parses
// what the transcript gained since, which matters for a working session's
// ever-growing transcript.
func EditedFiles(path string, since time.Time) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	edits.Lock()
	hit, ok := edits.byPath[path]
	edits.Unlock()
	if ok && hit.since.Equal(since) && hit.size == info.Size() {
		return hit.files, nil
	}
	from, older := max(info.Size()-4<<20, 0), []string(nil)
	if ok && hit.since.Equal(since) && hit.size < info.Size() {
		from, older = hit.size, hit.files
	}
	files, read, err := readEdits(path, from, info.Size(), since)
	if err != nil {
		return nil, err
	}
	files = mergeNewest(files, older)
	edits.Lock()
	edits.byPath[path] = editsEntry{read, since, files}
	edits.Unlock()
	return files, nil
}

// readEdits parses the whole lines between two offsets and says where the
// last of them ended, so a line still being written is read next time. A
// start mid-line skips to the next line.
func readEdits(path string, from, to int64, since time.Time) ([]string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, from, err
	}
	defer f.Close()
	start := max(from-1, 0)
	chunk := make([]byte, to-start)
	if _, err := f.ReadAt(chunk, start); err != nil {
		return nil, from, err
	}
	if from > 0 {
		skip := bytes.IndexByte(chunk, '\n')
		if skip < 0 {
			return nil, from, nil
		}
		chunk, start = chunk[skip+1:], start+int64(skip)+1
	}
	end := bytes.LastIndexByte(chunk, '\n') + 1
	lines := bytes.Split(chunk[:end], []byte{'\n'})
	var files []string
	for i := len(lines) - 1; i >= 0; i-- {
		if !bytes.Contains(lines[i], []byte(`"tool_use"`)) {
			continue
		}
		var e struct {
			Type      string    `json:"type"`
			Timestamp time.Time `json:"timestamp"`
			Message   struct {
				Content []struct {
					Type  string         `json:"type"`
					Name  string         `json:"name"`
					Input map[string]any `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(lines[i], &e) != nil || e.Type != "assistant" {
			continue
		}
		if e.Timestamp.Before(since) {
			break
		}
		for _, c := range e.Message.Content {
			if key, ok := editTools[c.Name]; ok && c.Type == "tool_use" {
				if file, _ := c.Input[key].(string); file != "" {
					files = mergeNewest(files, []string{file})
				}
			}
		}
	}
	return files, start + int64(end), nil
}

// mergeNewest appends older files after newer ones, keeping each once.
func mergeNewest(newer, older []string) []string {
	out := slices.Clone(newer)
	for _, f := range older {
		if !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	return out
}
