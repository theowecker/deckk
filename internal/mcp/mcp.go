package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Calls run concurrently, so a tool may block for a long time without holding
// up the others. A call's context ends when the client cancels it or the input
// closes.
func Serve(in io.Reader, out io.Writer, name, version string, tools []Tool) error {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	s := &server{out: out, tools: tools, name: name, version: version, calls: map[string]context.CancelFunc{}}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	var wg sync.WaitGroup
	for scanner.Scan() {
		var req request
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			continue
		}
		if req.ID == nil {
			s.notified(req)
			continue
		}
		callCtx, cancel := context.WithCancel(ctx)
		s.track(string(req.ID), cancel)
		wg.Go(func() {
			defer s.untrack(string(req.ID))
			s.handle(callCtx, req)
		})
	}
	stop()
	wg.Wait()
	return scanner.Err()
}

type server struct {
	mu      sync.Mutex
	out     io.Writer
	tools   []Tool
	name    string
	version string
	calls   map[string]context.CancelFunc
}

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *server) track(id string, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[id] = cancel
}

func (s *server) untrack(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel, ok := s.calls[id]; ok {
		cancel()
		delete(s.calls, id)
	}
}

func (s *server) notified(req request) {
	if req.Method != "notifications/cancelled" {
		return
	}
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(req.Params, &p) == nil {
		s.untrack(string(p.RequestID))
	}
}

func (s *server) handle(ctx context.Context, req request) {
	result, rerr := s.safeDispatch(ctx, req)
	reply := map[string]any{"jsonrpc": "2.0", "id": req.ID}
	if rerr != nil {
		reply["error"] = rerr
	} else {
		reply["result"] = result
	}
	data, err := json.Marshal(reply)
	if err != nil {
		data, _ = json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": rpcError{-32603, err.Error()}})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.out.Write(append(data, '\n'))
}

// A panicking tool fails its own call, not every call in flight.
func (s *server) safeDispatch(ctx context.Context, req request) (result any, rerr *rpcError) {
	defer func() {
		if r := recover(); r != nil {
			result, rerr = nil, &rpcError{-32603, fmt.Sprint("internal error: ", r)}
		}
	}()
	return s.dispatch(ctx, req)
}

func (s *server) dispatch(ctx context.Context, req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if p.ProtocolVersion == "" {
			p.ProtocolVersion = "2025-06-18"
		}
		return map[string]any{
			"protocolVersion": p.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.name, "version": s.version},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		list := make([]map[string]any, len(s.tools))
		for i, t := range s.tools {
			list[i] = map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.schema()}
		}
		return map[string]any{"tools": list}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(req.Params, &p) != nil {
			return nil, &rpcError{-32602, "bad params"}
		}
		for _, t := range s.tools {
			if t.Name == p.Name {
				var a Args
				_ = json.Unmarshal(p.Arguments, &a)
				text, err := t.Run(ctx, a)
				if err != nil {
					return toolResult(err.Error(), true), nil
				}
				return toolResult(text, false), nil
			}
		}
		return nil, &rpcError{-32602, fmt.Sprintf("no tool %q", p.Name)}
	}
	return nil, &rpcError{-32601, "method not found"}
}

func toolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

type Args map[string]any

func (a Args) String(key string) string {
	v, _ := a[key].(string)
	return v
}

func (a Args) Int(key string, fallback int) int {
	if v, ok := a[key].(float64); ok {
		return int(v)
	}
	return fallback
}

func (a Args) Bool(key string) bool {
	v, _ := a[key].(bool)
	return v
}

type Param struct {
	Name, Kind, Description string
	Required                bool
}

type Tool struct {
	Name, Description string
	Params            []Param
	Run               func(context.Context, Args) (string, error)
}

func (t Tool) schema() map[string]any {
	props := map[string]any{}
	required := []string{}
	for _, p := range t.Params {
		props[p.Name] = map[string]any{"type": p.Kind, "description": p.Description}
		if p.Required {
			required = append(required, p.Name)
		}
	}
	return map[string]any{"type": "object", "properties": props, "required": required}
}
