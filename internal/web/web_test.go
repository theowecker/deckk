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
	s := &Server{token: token, state: []byte("{}"), onAction: func(a Action) { got = append(got, a) }}
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
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/stream?t="+s.token, nil)
	req.Host = "localhost:4747"
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
