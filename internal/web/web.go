// Package web serves deckk's dashboard as a page on localhost. The terminal
// dashboard publishes its state here and receives the page's actions back.
package web

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed static
var static embed.FS

// Port is fixed so a bookmark keeps working; another free port is used when
// something else holds it.
const Port = 4747

// An Action is something the page asks the dashboard to do.
type Action struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

const (
	Open    = "open"
	Dismiss = "dismiss"
	Talk    = "talk"
)

type Server struct {
	token    string
	url      string
	onAction func(Action)

	mu      sync.Mutex
	state   []byte
	waiters []chan struct{}
}

// Start listens on localhost and serves until the process ends.
func Start(tokenPath string, onAction func(Action)) (*Server, error) {
	token, err := loadToken(tokenPath)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", Port))
	if err != nil {
		if ln, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			return nil, err
		}
	}
	s := &Server{token: token, onAction: onAction, state: []byte("{}")}
	s.url = fmt.Sprintf("http://localhost:%d/?t=%s", ln.Addr().(*net.TCPAddr).Port, token)
	go func() { _ = http.Serve(ln, s.routes(ln.Addr().(*net.TCPAddr).Port)) }()
	return s, nil
}

// URL opens the page, carrying the token it needs.
func (s *Server) URL() string { return s.url }

// Publish replaces the state the page shows and wakes pages waiting on it.
func (s *Server) Publish(state any) {
	data, err := json.Marshal(state)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if bytes.Equal(data, s.state) {
		return
	}
	s.state = data
	for _, w := range s.waiters {
		close(w)
	}
	s.waiters = nil
}

func (s *Server) snapshot() ([]byte, chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := make(chan struct{})
	s.waiters = append(s.waiters, changed)
	return s.state, changed
}

func (s *Server) routes(port int) http.Handler {
	files, _ := fs.Sub(static, "static")
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(files))
	mux.HandleFunc("GET /api/state", s.authed(s.serveState))
	mux.HandleFunc("GET /api/stream", s.authed(s.serveStream))
	mux.HandleFunc("POST /api/action", s.authed(s.serveAction))
	return localOnly(port, mux)
}

// localOnly refuses requests addressed to any other host name, so a page
// elsewhere can't reach deckk by rebinding its own name to 127.0.0.1.
func localOnly(port int, next http.Handler) http.Handler {
	allowed := map[string]bool{
		fmt.Sprintf("localhost:%d", port): true,
		fmt.Sprintf("127.0.0.1:%d", port): true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed[r.Host] {
			http.Error(w, "deckk only answers on localhost", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

// The token comes as a header, or in the query for EventSource, which can't
// set headers.
func (s *Server) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		given := r.Header.Get("X-Deckk-Token")
		if given == "" {
			given = r.URL.Query().Get("t")
		}
		if subtle.ConstantTimeCompare([]byte(given), []byte(s.token)) != 1 {
			http.Error(w, "open the page from deckk to get a working link", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) serveState(w http.ResponseWriter, _ *http.Request) {
	state, _ := s.snapshot()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(state)
}

// serveStream sends the state whenever it changes, with a comment every so
// often so idle connections stay open.
func (s *Server) serveStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	keepAlive := time.NewTicker(20 * time.Second)
	defer keepAlive.Stop()
	for {
		state, changed := s.snapshot()
		fmt.Fprintf(w, "data: %s\n\n", state)
		flusher.Flush()
	wait:
		for {
			select {
			case <-r.Context().Done():
				return
			case <-changed:
				break wait
			case <-keepAlive.C:
				fmt.Fprint(w, ": still here\n\n")
				flusher.Flush()
			}
		}
	}
}

func (s *Server) serveAction(w http.ResponseWriter, r *http.Request) {
	var a Action
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&a); err != nil {
		http.Error(w, "bad action", http.StatusBadRequest)
		return
	}
	switch a.Kind {
	case Open, Dismiss, Talk:
		s.onAction(a)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
	}
}

// loadToken keeps one token per install, so a bookmarked link keeps working
// across launches.
func loadToken(path string) (string, error) {
	if data, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(data))) >= 32 {
		return strings.TrimSpace(string(data)), nil
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return token, os.WriteFile(path, []byte(token+"\n"), 0o600)
}
