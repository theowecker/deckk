package web

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testServer(t *testing.T) (*Server, http.Handler, *[]Action) {
	t.Helper()
	token, err := loadToken(filepath.Join(t.TempDir(), "token"))
	if err != nil {
		t.Fatal(err)
	}
	var got []Action
	s := newServer(token, func(a Action) error { got = append(got, a); return nil })
	return s, s.routes(4747), &got
}

func request(h http.Handler, method, target, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Host = "localhost:4747"
	if token != "" {
		r.Header.Set("X-Deckk-Token", token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestTheAPINeedsTheToken(t *testing.T) {
	s, h, _ := testServer(t)
	s.Publish(map[string]int{"waiting": 2})
	if w := request(h, "GET", "/api/state", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", w.Code)
	}
	if w := request(h, "GET", "/api/state", "wrong", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", w.Code)
	}
	if w := request(h, "GET", "/api/state", s.token, ""); w.Code != http.StatusOK || w.Body.String() != `{"waiting":2}` {
		t.Fatalf("token: %d %s", w.Code, w.Body)
	}
	if w := request(h, "GET", "/", "", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "<title>deckk</title>") {
		t.Fatalf("the page itself needs no token: %d", w.Code)
	}
}

func TestOtherHostNamesAreRefused(t *testing.T) {
	s, h, _ := testServer(t)
	r := httptest.NewRequest("GET", "/api/state", nil)
	r.Host = "attacker.example:4747"
	r.Header.Set("X-Deckk-Token", s.token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d", w.Code)
	}
}

func TestActionsReachTheDashboard(t *testing.T) {
	s, h, got := testServer(t)
	if w := request(h, "POST", "/api/action", s.token, `{"kind":"open","id":"abc"}`); w.Code != http.StatusNoContent {
		t.Fatalf("got %d", w.Code)
	}
	if w := request(h, "POST", "/api/action", s.token, `{"kind":"rm -rf","id":"abc"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown kinds should be refused: %d", w.Code)
	}
	if len(*got) != 1 || (*got)[0] != (Action{Kind: Open, ID: "abc"}) {
		t.Fatalf("got %v", *got)
	}
}

func TestTheStreamSendsEachChange(t *testing.T) {
	s, h, _ := testServer(t)
	srv := httptest.NewServer(h)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/stream", nil)
	req.Host = "localhost:4747"
	req.Header.Set("X-Deckk-Token", s.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	lines := bufio.NewScanner(resp.Body)
	next := func() string {
		for lines.Scan() {
			if data, ok := strings.CutPrefix(lines.Text(), "data: "); ok {
				return data
			}
		}
		return ""
	}
	if got := next(); got != "{}" {
		t.Fatalf("first %q", got)
	}
	s.Publish(map[string]int{"n": 1})
	if got := next(); got != `{"n":1}` {
		t.Fatalf("after publish %q", got)
	}
}

func TestTheLinkKeepsItsTokenFromTheServer(t *testing.T) {
	s := newServer("0123456789abcdef0123456789abcdef", func(Action) error { return nil })
	s.url = "http://127.0.0.1:4747/#t=" + s.token
	if !strings.Contains(s.URL(), "/#t=") {
		t.Fatalf("the token belongs after #, got %s", s.URL())
	}
	h := s.routes(4747)
	if w := request(h, "GET", "/", "", ""); w.Header().Get("X-Frame-Options") != "DENY" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("the page must refuse to be framed")
	}
	if w := request(h, "GET", "/", "", ""); len(w.Result().Cookies()) != 0 {
		t.Fatal("deckk sets no cookies")
	}
}

func TestActionsWaitForTheDashboard(t *testing.T) {
	s := newServer("0123456789abcdef0123456789abcdef", func(Action) error { return ErrNotReady })
	w := request(s.routes(4747), "POST", "/api/action", s.token, `{"kind":"open","id":"x"}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d", w.Code)
	}
}

func TestClosedStreamsLeaveNothingBehind(t *testing.T) {
	s, h, _ := testServer(t)
	srv := httptest.NewServer(h)
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/stream", nil)
	req.Host = "localhost:4747"
	req.Header.Set("X-Deckk-Token", s.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = bufio.NewReader(resp.Body).ReadString('\n')
	cancel()
	_ = resp.Body.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		n := len(s.waiters)
		s.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("a closed stream left its waiter registered")
}
