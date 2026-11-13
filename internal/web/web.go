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

// ErrNotReady means the dashboard can't take actions yet.
var ErrNotReady = errors.New("the dashboard isn't ready")

const cookieName = "deckk"

type Server struct {
	token    string
	url      string
	onAction func(Action) error

	mu      sync.Mutex
	state   []byte
	waiters map[chan struct{}]bool
}

// Start listens on localhost and serves until the process ends.
func Start(tokenPath string, onAction func(Action) error) (*Server, error) {
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
	// 127.0.0.1 rather than localhost, which may resolve to an IPv6 address
	// some other program is listening on.
	s.url = fmt.Sprintf("http://127.0.0.1:%d/?t=%s", port, token)
	// No write timeout: the live stream stays open for as long as the page.
	server := &http.Server{Handler: s.routes(port), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(ln) }()
	return s, nil
}

func newServer(token string, onAction func(Action) error) *Server {
	return &Server{token: token, onAction: onAction, state: []byte("{}"), waiters: map[chan struct{}]bool{}}
}

// URL opens the page. The token in it is traded for a cookie on arrival.
func (s *Server) URL() string { return s.url }

// Publish replaces the state the page shows and wakes the pages watching.
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

// subscribe returns the state and a channel closed when it next changes.
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
	mux.Handle("GET /", s.arrive(http.FileServerFS(files)))
	mux.HandleFunc("GET /api/token", s.authed(s.serveToken))
	mux.HandleFunc("GET /api/state", s.authed(s.serveState))
	mux.HandleFunc("GET /api/stream", s.authed(s.serveStream))
	mux.HandleFunc("POST /api/action", s.authedByHeader(s.serveAction))
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
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

// arrive trades the token in deckk's link for a cookie and drops it from
// the address, so it stays out of history and off the screen, and a
// bookmark of the bare page keeps working.
func (s *Server) arrive(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if given := r.URL.Query().Get("t"); given != "" {
			if s.valid(given) {
				http.SetCookie(w, &http.Cookie{
					Name: cookieName, Value: s.token, Path: "/", MaxAge: 365 * 24 * 60 * 60,
					HttpOnly: true, SameSite: http.SameSiteStrictMode,
				})
			}
			http.Redirect(w, r, r.URL.Path, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) valid(given string) bool {
	return subtle.ConstantTimeCompare([]byte(given), []byte(s.token)) == 1
}

// authed accepts the cookie or the header. Reads are safe with the cookie
// alone: other origins can't read the replies without CORS headers.
func (s *Server) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie(cookieName)
		if !s.valid(r.Header.Get("X-Deckk-Token")) && (cookie == nil || !s.valid(cookie.Value)) {
			http.Error(w, "open the page from deckk to get a working link", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// authedByHeader is for changes. Other pages on localhost count as the same
// site, so a cookie alone could be sent along by them; a custom header can't
// be, since it needs a CORS preflight deckk never grants.
func (s *Server) authedByHeader(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.valid(r.Header.Get("X-Deckk-Token")) {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) serveToken(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": s.token})
}

func (s *Server) serveState(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(s.current())
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

func (s *Server) serveAction(w http.ResponseWriter, r *http.Request) {
	var a Action
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&a); err != nil {
		http.Error(w, "bad action", http.StatusBadRequest)
		return
	}
	switch a.Kind {
	case Open, Dismiss, Talk:
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}
	if err := s.onAction(a); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// loadToken keeps one token per install, readable only by the user.
func loadToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err == nil && len(strings.TrimSpace(string(data))) >= 32 {
		return strings.TrimSpace(string(data)), os.Chmod(path, 0o600)
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
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
