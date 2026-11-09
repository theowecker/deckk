package claude

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Transcript struct {
	ID          string
	Path        string
	ModTime     time.Time
	CustomTitle string
	AITitle     string
	AgentName   string
	FirstPrompt string
	LastPrompt  string
	LastReply   string
	PR          string
	PRURL       string
	Dir         string
	Cwd         string
	Branch      string
}

func (t Transcript) Session() Session {
	return Session{
		ID:         t.ID,
		Dir:        cmp.Or(t.Dir, t.Cwd),
		Cwd:        t.Cwd,
		Title:      firstNonEmpty(t.CustomTitle, t.AITitle, t.AgentName, truncate(t.FirstPrompt, 60)),
		LastPrompt: t.LastPrompt,
		LastReply:  t.LastReply,
		PR:         t.PR,
		PRURL:      t.PRURL,
		Branch:     t.Branch,
		UpdatedAt:  t.ModTime,
	}
}

func (t Transcript) complete() bool {
	return t.LastPrompt != "" && t.LastReply != "" && (t.CustomTitle != "" || t.AITitle != "" || t.AgentName != "")
}

func ScanTranscripts(dir string, since time.Time, keep map[string]Live) ([]Transcript, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*", "*.jsonl"))
	if err != nil {
		return nil, err
	}

	results := make([]*Transcript, len(paths))
	var wg sync.WaitGroup
	limit := make(chan struct{}, 8)
	for i, p := range paths {
		wg.Go(func() {
			limit <- struct{}{}
			defer func() { <-limit }()
			results[i] = scanOne(p, since, keep)
		})
	}
	wg.Wait()

	var transcripts []Transcript
	for _, t := range results {
		if t != nil {
			transcripts = append(transcripts, *t)
		}
	}
	return transcripts, nil
}

func scanOne(path string, since time.Time, keep map[string]Live) *Transcript {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if _, live := keep[id]; !live && info.ModTime().Before(since) {
		return nil
	}
	t, err := cache.read(path, info)
	if err != nil || t.LastPrompt == "" && t.FirstPrompt == "" {
		return nil
	}
	return &t
}

var cache = transcriptCache{entries: map[string]cached{}}

type transcriptCache struct {
	mu      sync.Mutex
	entries map[string]cached
}

type cached struct {
	size       int64
	modTime    time.Time
	transcript Transcript
}

func (c *transcriptCache) read(path string, info os.FileInfo) (Transcript, error) {
	c.mu.Lock()
	hit, ok := c.entries[path]
	c.mu.Unlock()
	if ok && hit.size == info.Size() && hit.modTime.Equal(info.ModTime()) {
		return hit.transcript, nil
	}
	t, err := ReadTranscript(path)
	if err != nil {
		return t, err
	}
	t.ModTime = info.ModTime()
	c.mu.Lock()
	c.entries[path] = cached{info.Size(), info.ModTime(), t}
	c.mu.Unlock()
	return t, nil
}

// Transcripts reach hundreds of megabytes. Claude rewrites title and prompt
// markers every turn, so the tail almost always holds them.
var tailWindows = []int64{256 << 10, 4 << 20}

func ReadTranscript(path string) (Transcript, error) {
	f, err := os.Open(path)
	if err != nil {
		return Transcript{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return Transcript{}, err
	}

	t := Transcript{ID: strings.TrimSuffix(filepath.Base(path), ".jsonl"), Path: path}
	for _, window := range tailWindows {
		chunk, whole, err := readTail(f, info.Size(), window)
		if err != nil {
			return t, err
		}
		t = Transcript{ID: t.ID, Path: path}
		scanBackward(&t, chunk)
		if whole || t.complete() {
			break
		}
	}
	// Resume only works from the starting folder, which only the head has.
	head, err := readHead(f, 256<<10)
	if err != nil {
		return t, err
	}
	latest := t.Cwd
	t.Cwd = ""
	scanForward(&t, head)
	t.Dir, t.Cwd = t.Cwd, cmp.Or(latest, t.Cwd)
	return t, nil
}

func readTail(f *os.File, size, window int64) ([]byte, bool, error) {
	offset := max(size-window, 0)
	buf := make([]byte, size-offset)
	if _, err := f.ReadAt(buf, offset); err != nil && err != io.EOF {
		return nil, false, err
	}
	if offset > 0 {
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			buf = buf[i+1:]
		}
	}
	return buf, offset == 0, nil
}

func readHead(f *os.File, n int64) ([]byte, error) {
	buf := make([]byte, n)
	read, err := f.ReadAt(buf, 0)
	if err != nil && err != io.EOF {
		return nil, err
	}
	buf = buf[:read]
	if i := bytes.LastIndexByte(buf, '\n'); i >= 0 && read == int(n) {
		buf = buf[:i]
	}
	return buf, nil
}

type entry struct {
	Type        string          `json:"type"`
	CustomTitle string          `json:"customTitle"`
	PRNumber    int             `json:"prNumber"`
	PRURL       string          `json:"prUrl"`
	AITitle     string          `json:"aiTitle"`
	AgentName   string          `json:"agentName"`
	LastPrompt  string          `json:"lastPrompt"`
	Cwd         string          `json:"cwd"`
	GitBranch   string          `json:"gitBranch"`
	IsMeta      bool            `json:"isMeta"`
	IsSidechain bool            `json:"isSidechain"`
	Message     json.RawMessage `json:"message"`
}

func scanBackward(t *Transcript, chunk []byte) {
	lines := bytes.Split(chunk, []byte{'\n'})
	for i := len(lines) - 1; i >= 0; i-- {
		var e entry
		if len(lines[i]) == 0 || json.Unmarshal(lines[i], &e) != nil {
			continue
		}
		apply(t, e, false)
		if t.complete() {
			return
		}
	}
}

func scanForward(t *Transcript, chunk []byte) {
	for line := range bytes.SplitSeq(chunk, []byte{'\n'}) {
		var e entry
		if len(line) == 0 || json.Unmarshal(line, &e) != nil {
			continue
		}
		apply(t, e, true)
		if t.Cwd != "" && (t.FirstPrompt != "" || t.CustomTitle != "" || t.AITitle != "" || t.AgentName != "") {
			return
		}
	}
}

func apply(t *Transcript, e entry, forward bool) {
	setOnce := func(dst *string, v string) {
		if *dst == "" {
			*dst = strings.Join(strings.Fields(v), " ")
		}
	}
	switch e.Type {
	case "custom-title":
		setOnce(&t.CustomTitle, e.CustomTitle)
	case "ai-title":
		setOnce(&t.AITitle, e.AITitle)
	case "agent-name":
		setOnce(&t.AgentName, e.AgentName)
	case "last-prompt":
		setOnce(&t.LastPrompt, e.LastPrompt)
	case "user":
		if forward && !e.IsMeta && !e.IsSidechain {
			setOnce(&t.FirstPrompt, promptText(e.Message))
		}
	case "pr-link":
		if e.PRNumber > 0 {
			setOnce(&t.PR, fmt.Sprintf("#%d", e.PRNumber))
			setOnce(&t.PRURL, e.PRURL)
		}
	case "assistant":
		if !forward && !e.IsSidechain {
			setOnce(&t.LastReply, replyText(e.Message))
		}
	}
	if !e.IsSidechain {
		setOnce(&t.Cwd, e.Cwd)
		setOnce(&t.Branch, e.GitBranch)
	}
}

func promptText(raw json.RawMessage) string {
	var m struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return cleanPrompt(s)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(m.Content, &parts) == nil {
		for _, p := range parts {
			if p.Type == "text" {
				return cleanPrompt(p.Text)
			}
		}
	}
	return ""
}

func replyText(raw json.RawMessage) string {
	var m struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	for i := len(m.Content) - 1; i >= 0; i-- {
		if c := m.Content[i]; c.Type == "text" {
			return strings.Join(strings.Fields(c.Text), " ")
		}
	}
	return ""
}

func cleanPrompt(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "<") {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
