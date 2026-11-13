// Package mcp is a minimal Model Context Protocol server over stdio: enough
// for Claude Code to list and call tools.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Serve speaks MCP: one JSON-RPC message per line. Calls run concurrently,
// so a tool may block for a long time without holding up the others.
func Serve(in io.Reader, out io.Writer, name, version string, tools []Tool) error {
	s := &server{out: out, tools: tools, name: name, version: version}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	var wg sync.WaitGroup
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		wg.Go(func() { s.handle(line) })
	}
	wg.Wait()
	return scanner.Err()
}

type server struct {
	mu      sync.Mutex
	out     io.Writer
	tools   []Tool
	name    string
	version string
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

func (s *server) handle(line []byte) {
	var req request
	if json.Unmarshal(line, &req) != nil || req.ID == nil {
		return
	}
	result, err := s.dispatch(req)
	reply := map[string]any{"jsonrpc": "2.0", "id": req.ID}
	if err != nil {
		reply["error"] = err
	} else {
		reply["result"] = result
	}
	data, _ := json.Marshal(reply)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.out.Write(append(data, '\n'))
}

func (s *server) dispatch(req request) (any, *rpcError) {
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
				text, err := t.Run(a)
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

// Args are a call's arguments as JSON decoded them.
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

// Param kinds are JSON Schema types.
type Param struct {
	Name, Kind, Description string
	Required                bool
}

type Tool struct {
	Name, Description string
	Params            []Param
	Run               func(Args) (string, error)
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
