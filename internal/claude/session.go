package claude

import (
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/theowecker/deckk/internal/git"
)

type Session struct {
	ID string
	// Dir is where the session started, which resume needs. Cwd is where the
	// work is now.
	Dir string
	Cwd string
	// Path is the transcript, when there is one yet.
	Path       string
	Title      string
	LastPrompt string
	LastReply  string
	Branch     string
	Model      string
	Effort     string
	Mode       string
	// Context is the tokens the latest turn sent.
	Context    int
	UpdatedAt  time.Time
	Background bool

	PID int
	// Peer is the name other Claude sessions use to message this one.
	Peer      string
	TTY       string
	Status    string
	StatusAt  time.Time
	StartedAt time.Time

	PR    string
	PRURL string

	// Only read for running sessions.
	Git git.Status
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

// Sessions in keep load whatever their age.
func Load(home string, since time.Time, keep map[string]bool) ([]Session, error) {
	live, err := ReadLive(filepath.Join(home, "sessions"))
	if err != nil {
		return nil, err
	}
	liveByID := make(map[string]Live, len(live))
	wanted := maps.Clone(keep)
	if wanted == nil {
		wanted = map[string]bool{}
	}
	for _, l := range live {
		liveByID[l.SessionID] = l
		wanted[l.SessionID] = true
	}

	transcripts, err := ScanTranscripts(filepath.Join(home, "projects"), since, wanted)
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
	var wg sync.WaitGroup
	for i, s := range sessions {
		if s.Running() {
			wg.Go(func() {
				if g, ok := git.Read(s.Cwd); ok {
					sessions[i].Git = g
					sessions[i].Branch = g.Branch
				}
			})
		}
	}
	wg.Wait()
	return sessions, nil
}
