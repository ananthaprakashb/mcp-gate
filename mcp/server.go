// Package mcp exposes a saga coordinator through MCP's JSON-RPC tools API.
package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/ananthaprakashb/semantic-saga-mcp/saga"
)

type Server struct {
	Coordinator  *saga.Coordinator
	MaxBodyBytes int64
}
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		writeRPC(w, http.StatusMethodNotAllowed, response{JSONRPC: "2.0", Error: &rpcError{Code: -32600, Message: "POST required"}})
		return
	}
	limit := s.MaxBodyBytes
	if limit <= 0 {
		limit = 1 << 20
	}
	var req request
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeRPC(w, http.StatusBadRequest, response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "invalid JSON-RPC request"}})
		return
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		writeRPC(w, http.StatusBadRequest, response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32600, Message: "invalid request"}})
		return
	}
	res := response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		res.Result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}, "serverInfo": map[string]any{"name": "semantic-saga-mcp", "version": "0.1.0"}}
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
		return
	case "ping":
		res.Result = map[string]any{}
	case "tools/list":
		res.Result = map[string]any{"tools": tools}
	case "tools/call":
		result, err := s.call(r, req.Params)
		if err != nil {
			res.Result = toolResult(nil, err)
		} else {
			res.Result = result
		}
	default:
		res.Error = &rpcError{Code: -32601, Message: "method not found"}
	}
	writeRPC(w, http.StatusOK, res)
}

func (s *Server) call(r *http.Request, raw json.RawMessage) (any, error) {
	if s.Coordinator == nil {
		return nil, fmt.Errorf("coordinator unavailable")
	}
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, fmt.Errorf("invalid tool arguments: %w", err)
	}
	id, _ := p.Arguments["saga_id"].(string)
	var value any
	var err error
	switch p.Name {
	case "saga_begin":
		value, err = s.Coordinator.Begin(id)
	case "saga_execute":
		action, _ := p.Arguments["action"].(string)
		value, err = s.Coordinator.Execute(r.Context(), id, action, p.Arguments["arguments"])
	case "saga_commit":
		value, err = s.Coordinator.Commit(id)
	case "saga_rollback":
		value, err = s.Coordinator.Rollback(r.Context(), id)
	case "saga_get":
		value, err = s.Coordinator.Get(id)
	default:
		return nil, fmt.Errorf("unknown tool %q", p.Name)
	}
	return toolResult(value, err), nil
}
func toolResult(value any, err error) map[string]any {
	if err != nil {
		return map[string]any{"isError": true, "content": []map[string]string{{"type": "text", "text": err.Error()}}}
	}
	b, _ := json.Marshal(value)
	return map[string]any{"content": []map[string]string{{"type": "text", "text": string(b)}}, "structuredContent": value}
}
func decode(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("one value required")
	}
	return nil
}
func writeRPC(w http.ResponseWriter, status int, v response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

var objectSchema = map[string]any{"type": "object", "properties": map[string]any{"saga_id": map[string]any{"type": "string", "description": "Unique workflow identifier"}}, "required": []string{"saga_id"}, "additionalProperties": false}
var tools = []map[string]any{
	{"name": "saga_begin", "description": "Begin a logical workflow before making side effects.", "inputSchema": objectSchema},
	{"name": "saga_execute", "description": "Execute a configured action and record its compensation data. A failed action automatically rolls back prior steps.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"saga_id": map[string]any{"type": "string"}, "action": map[string]any{"type": "string"}, "arguments": map[string]any{"type": "object"}}, "required": []string{"saga_id", "action", "arguments"}, "additionalProperties": false}},
	{"name": "saga_commit", "description": "Commit a successful workflow, preventing later rollback.", "inputSchema": objectSchema},
	{"name": "saga_rollback", "description": "Compensate completed actions in reverse order. Safe to retry after compensation failures.", "inputSchema": objectSchema},
	{"name": "saga_get", "description": "Read workflow status and its execution log.", "inputSchema": objectSchema},
}
