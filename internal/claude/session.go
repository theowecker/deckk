package claude

import (
	"os"
	"path/filepath"
	"time"
)

type Session struct {
	ID string
	// Dir is where the session started, which resume needs. Cwd is where the
	// work is now.
	Dir        string
	Cwd        string
	Title      string
	LastPrompt string
	LastReply  string
	Branch     string
	UpdatedAt  time.Time
	Background bool

	PID       int
	TTY       string
	Status    string
	StatusAt  time.Time
	StartedAt time.Time

	PR    string
	PRURL string
}

func ShortID(id string) string {
	return id[:min(len(id), 8)]
}

func (s Session) Running() bool { return s.PID > 0 }

func (s Session) Project() string { return filepath.Base(s.Cwd) }

func Home() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

func Load(home string, since time.Time) ([]Session, error) {
	live, err := ReadLive(filepath.Join(home, "sessions"))
	if err != nil {
		return nil, err
	}
	liveByID := make(map[string]Live, len(live))
	for _, l := range live {
		liveByID[l.SessionID] = l
	}

	transcripts, err := ScanTranscripts(filepath.Join(home, "projects"), since, liveByID)
	if err != nil {
		return nil, err
	}

	sessions := make([]Session, 0, len(transcripts)+len(live))
	seen := make(map[string]bool)
	for _, t := range transcripts {
		s := t.Session()
		if l, ok := liveByID[s.ID]; ok {
			l.applyTo(&s)
		}
		seen[s.ID] = true
		sessions = append(sessions, s)
	}
	for _, l := range live {
		if !seen[l.SessionID] {
			s := Session{ID: l.SessionID}
			l.applyTo(&s)
			sessions = append(sessions, s)
		}
	}
	return sessions, nil
}
