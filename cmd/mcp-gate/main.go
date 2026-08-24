package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/example/mcp-gate/gate"
)

func main() {
	var routes []gate.Route
	if err := json.Unmarshal([]byte(os.Getenv("GATE_ROUTES")), &routes); err != nil {
		slog.Error("invalid route configuration", "error", err)
		os.Exit(1)
	}
	maxBody, _ := strconv.ParseInt(env("GATE_MAX_BODY_BYTES", "1048576"), 10, 64)
	g, err := gate.New(gate.Config{SigningKey: os.Getenv("GATE_SIGNING_KEY"), AdminKey: os.Getenv("GATE_ADMIN_KEY"), Routes: routes, MaxBodyBytes: maxBody})
	if err != nil {
		slog.Error("gate initialization failed", "error", err)
		os.Exit(1)
	}
	addr := env("GATE_ADDR", ":8080")
	slog.Info("mcp-gate listening", "address", addr)
	s := &http.Server{Addr: addr, Handler: g, ReadHeaderTimeout: 5e9, MaxHeaderBytes: 32 << 10}
	if err := s.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
