package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/example/mcp-gate/gate"
)

func main() {
	var routes []gate.Route
	if err := json.Unmarshal([]byte(os.Getenv("GATE_ROUTES")), &routes); err != nil {
		log.Fatal("GATE_ROUTES must be a JSON route array: ", err)
	}
	maxBody, _ := strconv.ParseInt(env("GATE_MAX_BODY_BYTES", "1048576"), 10, 64)
	g, err := gate.New(gate.Config{SigningKey: os.Getenv("GATE_SIGNING_KEY"), AdminKey: os.Getenv("GATE_ADMIN_KEY"), Routes: routes, MaxBodyBytes: maxBody})
	if err != nil {
		log.Fatal(err)
	}
	addr := env("GATE_ADDR", ":8080")
	log.Printf("mcp-gate listening on %s", addr)
	s := &http.Server{Addr: addr, Handler: g, ReadHeaderTimeout: 5e9, MaxHeaderBytes: 32 << 10}
	log.Fatal(s.ListenAndServe())
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
