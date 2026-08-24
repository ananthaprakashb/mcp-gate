package gate

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type failingReplayStore struct{}

func (failingReplayStore) Consume(context.Context, string, time.Time) (bool, error) {
	return false, io.ErrClosedPipe
}

func TestIssueProxyAndPreventReplay(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()
	now := time.Unix(1000, 0)
	g, err := New(Config{
		SigningKey: strings.Repeat("s", 32), AdminKey: "admin", Now: func() time.Time { return now },
		Routes: []Route{{
			Name: "tickets", Upstream: upstream.URL, PathPrefix: "/tickets", Methods: []string{"POST"}, MaxTTLSeconds: 30, UpstreamBearer: "secret",
			RequestSchema: &Schema{Type: "object", Properties: map[string]*Schema{"title": &Schema{Type: "string"}}, Required: []string{"title"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	issue := httptest.NewRequest("POST", "/v1/tokens", strings.NewReader(`{"route":"tickets","method":"POST","path":"/tickets","ttl_seconds":10}`))
	issue.Header.Set("X-Gate-Key", "admin")
	iw := httptest.NewRecorder()
	g.ServeHTTP(iw, issue)
	if iw.Code != 200 {
		t.Fatalf("issue: %d %s", iw.Code, iw.Body.String())
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(iw.Body.Bytes(), &token); err != nil {
		t.Fatal(err)
	}
	call := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/proxy/tickets/tickets", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token.AccessToken)
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		return w
	}
	if w := call(`{"title":"hello","extra":1}`); w.Code != 422 {
		t.Fatalf("schema status=%d", w.Code)
	}
	if w := call(`{"title":"hello"}`); w.Code != 201 {
		t.Fatalf("proxy status=%d %s", w.Code, w.Body.String())
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("upstream auth=%q", gotAuth)
	}
	if w := call(`{"title":"again"}`); w.Code != 401 {
		t.Fatalf("replay status=%d", w.Code)
	}
}

func TestHealthEndpoints(t *testing.T) {
	g, err := New(Config{SigningKey: strings.Repeat("s", 32), AdminKey: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		w := httptest.NewRecorder()
		g.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK || w.Body.String() != "{\"status\":\"ok\"}\n" {
			t.Fatalf("%s: status=%d body=%q", path, w.Code, w.Body.String())
		}
	}
}

func TestUpstreamAuthAndSafeErrors(t *testing.T) {
	var gotHeader, gotQuery, gotUser, gotPass string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader, gotQuery = r.Header.Get("X-API-Key"), r.URL.Query().Get("tenant")
		gotUser, gotPass, _ = r.BasicAuth()
		http.Error(w, "private stack trace: database.internal", http.StatusBadGateway)
	}))
	defer upstream.Close()
	g, err := New(Config{SigningKey: strings.Repeat("s", 32), AdminKey: "admin", Routes: []Route{{
		Name: "x", Upstream: upstream.URL, PathPrefix: "/x", Methods: []string{"POST"}, MaxTTLSeconds: 30,
		RequestSchema: &Schema{Type: "object", Properties: map[string]*Schema{}},
		UpstreamAuth:  UpstreamAuth{BasicUser: "user", BasicPass: "pass", Headers: map[string]string{"X-API-Key": "secret"}, Query: map[string]string{"tenant": "acme"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	token, _ := g.sign(claims{Route: "x", Method: "POST", Path: "/x", JTI: "safe", Exp: time.Now().Add(time.Minute).Unix()})
	r := httptest.NewRequest("POST", "/proxy/x/x", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if gotHeader != "secret" || gotQuery != "acme" || gotUser != "user" || gotPass != "pass" {
		t.Fatalf("auth was not injected")
	}
	if w.Code != http.StatusBadGateway || strings.Contains(w.Body.String(), "database.internal") {
		t.Fatalf("unsafe response: %d %s", w.Code, w.Body.String())
	}
}

func TestReplayStoreFailureFailsClosed(t *testing.T) {
	g, err := New(Config{SigningKey: strings.Repeat("s", 32), AdminKey: "admin", ReplayStore: failingReplayStore{}, Routes: []Route{{
		Name: "x", Upstream: "https://example.com", PathPrefix: "/x", Methods: []string{"POST"}, MaxTTLSeconds: 30,
		RequestSchema: &Schema{Type: "object", Properties: map[string]*Schema{}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	token, _ := g.sign(claims{Route: "x", Method: "POST", Path: "/x", JTI: "fail", Exp: time.Now().Add(time.Minute).Unix()})
	r := httptest.NewRequest("POST", "/proxy/x/x", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestExpiredAndScopedToken(t *testing.T) {
	now := time.Unix(1000, 0)
	g, _ := New(Config{
		SigningKey: strings.Repeat("k", 32), AdminKey: "a", Now: func() time.Time { return now },
		Routes: []Route{{
			Name: "x", Upstream: "https://example.com", PathPrefix: "/x", Methods: []string{"POST"}, MaxTTLSeconds: 2,
			RequestSchema: &Schema{Type: "object", Properties: map[string]*Schema{}},
		}},
	})
	tok, _ := g.sign(claims{Route: "x", Method: "POST", Path: "/x", JTI: "1", Exp: 1001})
	now = time.Unix(1002, 0)
	r := httptest.NewRequest("POST", "/proxy/x/x", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 401 {
		b, _ := io.ReadAll(w.Body)
		t.Fatalf("status=%d %s", w.Code, b)
	}
}
