package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ananthaprakashb/semantic-saga-mcp/saga"
)

type executor struct{}

func (executor) Execute(context.Context, saga.Action, string, int, any) (any, error) {
	return map[string]any{"id": "created"}, nil
}
func (executor) Compensate(context.Context, saga.Action, string, saga.Step) error { return nil }
func TestMCPToolsWorkflow(t *testing.T) {
	c, err := saga.New([]saga.Action{{Name: "create", ExecuteURL: "http://execute", CompensateURL: "http://undo"}}, executor{})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{Coordinator: c}
	call := func(body string) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		server.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body)))
		if w.Code != 200 {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	listed := call(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools := listed["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 5 {
		t.Fatalf("tools=%d", len(tools))
	}
	begin := call(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"saga_begin","arguments":{"saga_id":"wf-1"}}}`)
	if begin["error"] != nil {
		t.Fatalf("begin=%v", begin)
	}
	execute := call(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"saga_execute","arguments":{"saga_id":"wf-1","action":"create","arguments":{"name":"demo"}}}}`)
	result := execute["result"].(map[string]any)
	if result["isError"] == true {
		t.Fatalf("execute=%v", execute)
	}
}
func TestMCPRejectsUnknownJSONRPCFields(t *testing.T) {
	server := &Server{}
	w := httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping","surprise":true}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
