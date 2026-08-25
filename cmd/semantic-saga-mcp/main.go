package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/ananthaprakashb/semantic-saga-mcp/mcp"
	"github.com/ananthaprakashb/semantic-saga-mcp/saga"
)

func main() {
	var actions []saga.Action
	if err := json.Unmarshal([]byte(os.Getenv("SAGA_ACTIONS")), &actions); err != nil {
		slog.Error("invalid SAGA_ACTIONS", "error", err)
		os.Exit(1)
	}
	executor := saga.HTTPExecutor{Client: &http.Client{Timeout: 30 * time.Second}}
	coordinator, err := saga.New(actions, executor)
	if err != nil {
		slog.Error("coordinator initialization failed", "error", err)
		os.Exit(1)
	}
	addr := os.Getenv("SAGA_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	server := &http.Server{Addr: addr, Handler: &mcp.Server{Coordinator: coordinator}, ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 32 << 10}
	slog.Info("semantic saga MCP listening", "address", addr)
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
