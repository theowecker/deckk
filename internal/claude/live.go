package claude

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Live struct {
	PID        int    `json:"pid"`
	SessionID  string `json:"sessionId"`
	Cwd        string `json:"cwd"`
	Name       string `json:"name"`
	NameSource string `json:"nameSource"`
	Status     string `json:"status"`
	UpdatedAt  int64  `json:"updatedAt"`
	StartedAt  int64  `json:"startedAt"`
	StatusAt   int64  `json:"statusUpdatedAt"`
	ProcStart  string `json:"procStart"`
	Entrypoint string `json:"entrypoint"`
	// Kind is "interactive" for a session someone is typing into. Older
	// versions of Claude don't write it.
	Kind string `json:"kind"`
	TTY  string `json:"-"`
}

func (l Live) applyTo(s *Session) {
	s.PID = l.PID
	s.Peer = l.Name
	s.TTY = l.TTY
	s.Status = l.Status
	if l.Entrypoint != "" {
		s.Background = isBackground(l.Entrypoint)
	}
	if l.Kind != "" && l.Kind != "interactive" {
		s.Background = true
	}
	if l.StartedAt > 0 {
		s.StartedAt = time.UnixMilli(l.StartedAt)
	}
	if l.StatusAt > 0 {
		s.StatusAt = time.UnixMilli(l.StatusAt)
	}
	if s.Cwd == "" {
		s.Cwd = l.Cwd
	}
	if s.Dir == "" {
		s.Dir = l.Cwd
	}
	// "derived" names are placeholders like "dev-a9" made from the folder.
	if l.NameSource == "user" || s.Title == "" && l.NameSource != "derived" {
		s.Title = l.Name
	}
	if t := time.UnixMilli(max(l.UpdatedAt, l.StartedAt)); t.After(s.UpdatedAt) {
		s.UpdatedAt = t
	}
}

// Claude leaves stale registry files after crashes and PIDs get reused, so
// the process start time has to match too.
func ReadLive(dir string) ([]Live, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}

	var entries []Live
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		var l Live
		if json.Unmarshal(data, &l) != nil || l.PID == 0 || l.SessionID == "" {
			continue
		}
		entries = append(entries, l)
	}
	if len(entries) == 0 {
		return nil, nil
	}

	procs := processes(entries)
	live := entries[:0]
	for _, l := range entries {
		if p, ok := procs[l.PID]; ok && p.claude && sameStart(l.ProcStart, p.started) {
			l.TTY = p.tty
			live = append(live, l)
		}
	}
	return live, nil
}

type process struct {
	tty     string
	started time.Time
	claude  bool
}

const startLayout = "Mon Jan _2 15:04:05 2006"

// Claude records the start in UTC while ps reports local time.
func sameStart(recorded string, actual time.Time) bool {
	if recorded == "" {
		return true
	}
	t, err := time.Parse(startLayout, recorded)
	if err != nil {
		return true
	}
	d := t.Sub(actual)
	return d > -2*time.Second && d < 2*time.Second
}

func processes(entries []Live) map[int]process {
	pids := make([]string, len(entries))
	for i, l := range entries {
		pids[i] = strconv.Itoa(l.PID)
	}
	out, _ := exec.Command("ps", "-o", "pid=,tty=,lstart=,comm=", "-p", strings.Join(pids, ",")).Output()
	return parsePS(string(out))
}

func parsePS(out string) map[int]process {
	procs := make(map[int]process)
	for line := range strings.Lines(out) {
		// lstart is always five fields, like "Mon Nov  9 16:42:09 2026".
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		tty := fields[1]
		if tty == "??" {
			tty = ""
		} else if !strings.HasPrefix(tty, "/dev/") {
			tty = "/dev/" + tty
		}
		started, _ := time.ParseInLocation(startLayout, strings.Join(fields[2:7], " "), time.Local)
		comm := filepath.Base(strings.Join(fields[7:], " "))
		procs[pid] = process{tty: tty, started: started, claude: strings.Contains(comm, "claude")}
	}
	return procs
}
