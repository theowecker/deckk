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
	"maps"
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

type Action struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
	// Text is a prompt, a message for the deckk session, a new name or the
	// goals, depending on the kind.
	Text string `json:"text,omitempty"`
	// Send presses Enter after a prompt instead of leaving it to review.
	Send   bool   `json:"send,omitempty"`
	Folder string `json:"folder,omitempty"`
	// Confirm closes a session even while Claude is working.
	Confirm bool `json:"confirm,omitempty"`
}

const (
	Open    = "open"
	Dismiss = "dismiss"
	Talk    = "talk"
	Pin     = "pin"
	Archive = "archive"
	Rename  = "rename"
	Prompt  = "prompt"
	Chat    = "chat"
	Goals   = "goals"
	Trust   = "trust"
	New     = "new"
	Close   = "close"
)

var Kinds = map[string]bool{Open: true, Dismiss: true, Talk: true, Pin: true, Archive: true, Rename: true, Prompt: true, Chat: true, Goals: true, Trust: true, New: true, Close: true}

type Search func(query string) ([]Found, error)

type Found struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Project    string    `json:"project"`
	Folder     string    `json:"folder"`
	Branch     string    `json:"branch,omitempty"`
	Updated    time.Time `json:"updated"`
	LastPrompt string    `json:"lastPrompt,omitempty"`
	Running    bool      `json:"running,omitempty"`
	Background bool      `json:"background,omitempty"`
	Pinned     bool      `json:"pinned,omitempty"`
	Archived   bool      `json:"archived,omitempty"`
	// Mentioned matches only somewhere in the conversation, not in what the
	// session is called or last said.
	Mentioned bool `json:"mentioned,omitempty"`
}

var ErrNotReady = errors.New("the dashboard isn't ready")

type History func(id string) ([]Turn, error)

type Turn struct {
	Prompt string `json:"prompt"`
	Reply  string `json:"reply"`
}

type Server struct {
	token    string
	base     string
	onAction func(Action) error
	history  History
	search   Search
	server   *http.Server

	mu      sync.Mutex
	state   []byte
	waiters map[chan struct{}]bool
	codes   map[string]time.Time
}

func Start(tokenPath string, onAction func(Action) error, history History, search Search) (*Server, error) {
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
	port := ln.Addr().(*net.TCPAddr).Port
	s := newServer(token, onAction)
	s.history, s.search = history, search
	// 127.0.0.1 rather than localhost, which may resolve to an IPv6 address
	// some other program is listening on.
	s.base = fmt.Sprintf("http://127.0.0.1:%d/", port)
	// No write timeout: the live stream stays open for as long as the page.
	s.server = &http.Server{Handler: s.routes(port), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = s.server.Serve(ln) }()
	return s, nil
}

func (s *Server) Close() error { return s.server.Close() }

func newServer(token string, onAction func(Action) error) *Server {
	return &Server{token: token, onAction: onAction, state: []byte("{}"), waiters: map[chan struct{}]bool{}, codes: map[string]time.Time{}}
}

// The link carries a code the page trades once for the token, after #, which
// browsers never send to a server. Browser history keeps only spent codes.
func (s *Server) URL() string {
	code := randomHex(16)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	maps.DeleteFunc(s.codes, func(_ string, at time.Time) bool { return now.Sub(at) > codeFor })
	s.codes[code] = now
	return s.base + "#c=" + code
}

const codeFor = 2 * time.Minute

func (s *Server) serveClaim(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	at, ok := s.codes[body.Code]
	delete(s.codes, body.Code)
	s.mu.Unlock()
	if !ok || time.Since(at) > codeFor {
		http.Error(w, "This link was already used. Press w in deckk for a new one.", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": s.token})
}

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
	for w := range s.waiters {
		close(w)
	}
	clear(s.waiters)
}

func (s *Server) current() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Registering under the lock means no change slips by between the two.
func (s *Server) subscribe() ([]byte, chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := make(chan struct{})
	s.waiters[changed] = true
	return s.state, changed
}

func (s *Server) unsubscribe(changed chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.waiters, changed)
}

func (s *Server) routes(port int) http.Handler {
	files, _ := fs.Sub(static, "static")
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(files))
	mux.HandleFunc("GET /api/state", s.authed(s.serveState))
	mux.HandleFunc("GET /api/stream", s.authed(s.serveStream))
	mux.HandleFunc("GET /api/history", s.authed(s.serveHistory))
	mux.HandleFunc("GET /api/search", s.authed(s.serveSearch))
	mux.HandleFunc("POST /api/action", s.authed(s.serveAction))
	mux.HandleFunc("POST /api/claim", s.serveClaim)
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
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) valid(given string) bool {
	return subtle.ConstantTimeCompare([]byte(given), []byte(s.token)) == 1
}

// authed needs the token in a header. A page on another site can't add one
// without a CORS preflight, which deckk never grants, and deckk sets no
// cookie, since cookies would reach every other server on 127.0.0.1.
func (s *Server) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.valid(r.Header.Get("X-Deckk-Token")) {
			http.Error(w, "This page needs a fresh link. Press w in deckk.", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) serveState(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(s.current())
}

// A comment every so often keeps idle connections open.
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
		state, changed := s.subscribe()
		fmt.Fprintf(w, "data: %s\n\n", state)
		flusher.Flush()
	wait:
		for {
			select {
			case <-r.Context().Done():
				s.unsubscribe(changed)
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

func (s *Server) serveHistory(w http.ResponseWriter, r *http.Request) {
	if s.history == nil {
		http.Error(w, "no history here", http.StatusNotFound)
		return
	}
	turns, err := s.history(r.URL.Query().Get("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if turns == nil {
		turns = []Turn{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(turns)
}

func (s *Server) serveSearch(w http.ResponseWriter, r *http.Request) {
	if s.search == nil {
		http.Error(w, "no search here", http.StatusNotFound)
		return
	}
	found, err := s.search(r.URL.Query().Get("q"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if found == nil {
		found = []Found{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(found)
}

func (s *Server) serveAction(w http.ResponseWriter, r *http.Request) {
	var a Action
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&a); err != nil {
		http.Error(w, "bad action", http.StatusBadRequest)
		return
	}
	if !Kinds[a.Kind] {
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}
	if err := s.onAction(a); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func loadToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err == nil && len(strings.TrimSpace(string(data))) >= 32 {
		return strings.TrimSpace(string(data)), os.Chmod(path, 0o600)
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	token := randomHex(24)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".web.token.*")
	if err != nil {
		return "", err
	}
	if _, err := tmp.WriteString(token + "\n"); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return token, os.Rename(tmp.Name(), path)
}

// crypto/rand.Read never fails on macOS.
func randomHex(n int) string {
	raw := make([]byte, n)
	_, _ = rand.Read(raw)
	return hex.EncodeToString(raw)
}
