package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestServeListsAndCallsTools(t *testing.T) {
	tools := []Tool{
		{Name: "echo", Description: "says it back", Params: []Param{{Name: "text", Kind: "string", Required: true}},
			Run: func(_ context.Context, a Args) (string, error) { return a.String("text"), nil }},
		{Name: "fail", Run: func(context.Context, Args) (string, error) { return "", errors.New("nope") }},
		{Name: "panic", Run: func(context.Context, Args) (string, error) { panic("boom") }},
	}
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"text":"hi"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"fail"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"nope"}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"panic"}}`,
	}, "\n") + "\n"
	r, w := io.Pipe()
	go func() {
		_ = Serve(strings.NewReader(in), w, "test", "1", tools)
		_ = w.Close()
	}()
	replies := map[float64]map[string]any{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		var m map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		replies[m["id"].(float64)] = m
	}
	if len(replies) != 6 {
		t.Fatalf("replies %v", replies)
	}
	if v := replies[1]["result"].(map[string]any)["protocolVersion"]; v != "2025-06-18" {
		t.Fatalf("version %v", v)
	}
	if n := len(replies[2]["result"].(map[string]any)["tools"].([]any)); n != 3 {
		t.Fatalf("%d tools", n)
	}
	text := func(id float64) (string, bool) {
		res := replies[id]["result"].(map[string]any)
		return res["content"].([]any)[0].(map[string]any)["text"].(string), res["isError"].(bool)
	}
	if got, isErr := text(3); got != "hi" || isErr {
		t.Fatalf("echo %q %v", got, isErr)
	}
	if got, isErr := text(4); got != "nope" || !isErr {
		t.Fatalf("fail %q %v", got, isErr)
	}
	if replies[5]["error"] == nil {
		t.Fatal("unknown methods should error")
	}
	if replies[6]["error"] == nil {
		t.Fatal("a panicking tool should fail its call")
	}
}

func TestACancelledCallStops(t *testing.T) {
	started := make(chan struct{})
	tools := []Tool{{Name: "wait", Run: func(ctx context.Context, _ Args) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}}}
	in, feed := io.Pipe()
	out, w := io.Pipe()
	done := make(chan struct{})
	go func() {
		_ = Serve(in, w, "test", "1", tools)
		_ = w.Close()
		close(done)
	}()
	go func() { _, _ = io.Copy(io.Discard, out) }()
	_, _ = feed.Write([]byte(`{"jsonrpc":"2.0","id":"a","method":"tools/call","params":{"name":"wait"}}` + "\n"))
	<-started
	_, _ = feed.Write([]byte(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"a"}}` + "\n"))
	_ = feed.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the server kept waiting on a cancelled call")
	}
}
