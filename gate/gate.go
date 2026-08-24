package gate

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Route struct {
	Name           string       `json:"name"`
	Upstream       string       `json:"upstream"`
	PathPrefix     string       `json:"path_prefix"`
	Methods        []string     `json:"methods"`
	MaxTTLSeconds  int          `json:"max_ttl_seconds"`
	RequestSchema  *Schema      `json:"request_schema"`
	UpstreamBearer string       `json:"upstream_bearer,omitempty"`
	UpstreamAuth   UpstreamAuth `json:"upstream_auth,omitempty"`
}

// UpstreamAuth describes credentials added by the gate after caller headers
// have been discarded. Header values should be supplied through secrets, not
// included in capability tokens.
type UpstreamAuth struct {
	Bearer    string            `json:"bearer,omitempty"`
	BasicUser string            `json:"basic_user,omitempty"`
	BasicPass string            `json:"basic_pass,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Query     map[string]string `json:"query,omitempty"`
}

// ReplayStore atomically records a token identifier until its expiry. A
// distributed implementation can use Redis SET NX with the corresponding TTL.
type ReplayStore interface {
	Consume(ctx context.Context, id string, expiresAt time.Time) (bool, error)
}

type Config struct {
	SigningKey, AdminKey string
	Routes               []Route
	MaxBodyBytes         int64
	Now                  func() time.Time
	ReplayStore          ReplayStore
	HTTPClient           *http.Client
}
type Gate struct {
	cfg    Config
	routes map[string]Route
	client *http.Client
	replay ReplayStore
}
type claims struct {
	Route, Method, Path, JTI string
	Exp                      int64
}

func New(cfg Config) (*Gate, error) {
	if len(cfg.SigningKey) < 32 {
		return nil, errors.New("signing key must be at least 32 characters")
	}
	if cfg.AdminKey == "" {
		return nil, errors.New("admin key is required")
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = 1 << 20
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.ReplayStore == nil {
		cfg.ReplayStore = newMemoryReplayStore(cfg.Now)
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	g := &Gate{cfg: cfg, routes: map[string]Route{}, replay: cfg.ReplayStore, client: cfg.HTTPClient}
	for _, r := range cfg.Routes {
		u, err := url.Parse(r.Upstream)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("route %q has invalid upstream", r.Name)
		}
		if r.Name == "" || r.RequestSchema == nil || r.MaxTTLSeconds < 1 || r.MaxTTLSeconds > 300 {
			return nil, fmt.Errorf("route %q has invalid policy", r.Name)
		}
		if r.UpstreamBearer != "" && r.UpstreamAuth.Bearer != "" {
			return nil, fmt.Errorf("route %q configures bearer authentication twice", r.Name)
		}
		for name := range r.UpstreamAuth.Headers {
			if !validUpstreamHeader(name) {
				return nil, fmt.Errorf("route %q has forbidden upstream header %q", r.Name, name)
			}
		}
		if r.PathPrefix == "" {
			r.PathPrefix = "/"
		}
		g.routes[r.Name] = r
	}
	return g, nil
}

func (g *Gate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`+"\n")
		return
	}
	if r.URL.Path == "/v1/tokens" {
		g.issue(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/proxy/") {
		g.proxy(w, r)
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
}

func (g *Gate) issue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method_not_allowed", "POST required")
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Gate-Key")), []byte(g.cfg.AdminKey)) != 1 {
		writeError(w, 401, "unauthorized", "invalid gate key")
		return
	}
	var in struct {
		Route, Method, Path string
		TTLSeconds          int `json:"ttl_seconds"`
	}
	if err := decodeStrict(io.LimitReader(r.Body, 16<<10), &in); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	policy, ok := g.routes[in.Route]
	if !ok {
		writeError(w, 400, "invalid_route", "unknown route")
		return
	}
	in.Method = strings.ToUpper(in.Method)
	if !contains(policy.Methods, in.Method) || !validPath(in.Path, policy.PathPrefix) {
		writeError(w, 403, "scope_denied", "method or path is outside route policy")
		return
	}
	if in.TTLSeconds < 1 || in.TTLSeconds > policy.MaxTTLSeconds {
		writeError(w, 400, "invalid_ttl", fmt.Sprintf("ttl_seconds must be between 1 and %d", policy.MaxTTLSeconds))
		return
	}
	jtiBytes := make([]byte, 18)
	if _, err := rand.Read(jtiBytes); err != nil {
		writeError(w, 500, "internal_error", "could not create token")
		return
	}
	c := claims{in.Route, in.Method, in.Path, base64.RawURLEncoding.EncodeToString(jtiBytes), g.cfg.Now().Add(time.Duration(in.TTLSeconds) * time.Second).Unix()}
	token, _ := g.sign(c)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": in.TTLSeconds, "scope": in.Method + " " + in.Path})
}

func (g *Gate) proxy(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	c, err := g.verify(token)
	if err != nil {
		writeError(w, 401, "invalid_token", err.Error())
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/proxy/")
	if i := strings.IndexByte(name, '/'); i >= 0 {
		name = name[:i]
	}
	if name != c.Route || r.Method != c.Method || r.URL.Path != "/proxy/"+c.Route+c.Path || r.URL.RawQuery != "" {
		writeError(w, 403, "scope_denied", "request does not match token scope")
		return
	}
	policy := g.routes[c.Route]
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, g.cfg.MaxBodyBytes))
	if err != nil {
		writeError(w, 413, "body_too_large", "request body exceeds limit")
		return
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		writeError(w, 400, "invalid_json", "body must be valid JSON")
		return
	}
	if err := policy.RequestSchema.Validate(value); err != nil {
		writeError(w, 422, "schema_violation", err.Error())
		return
	}
	consumed, err := g.replay.Consume(r.Context(), c.JTI, time.Unix(c.Exp, 0))
	if err != nil {
		writeError(w, 503, "replay_store_unavailable", "token replay protection is unavailable")
		return
	}
	if !consumed {
		writeError(w, 401, "token_replayed", "token has already been used")
		return
	}
	base, _ := url.Parse(policy.Upstream)
	target := base.ResolveReference(&url.URL{Path: c.Path})
	query := target.Query()
	for key, value := range policy.UpstreamAuth.Query {
		query.Set(key, value)
	}
	target.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), bytes.NewReader(body))
	if err != nil {
		writeError(w, 500, "internal_error", "could not build upstream request")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for name, value := range policy.UpstreamAuth.Headers {
		req.Header.Set(name, value)
	}
	bearer := policy.UpstreamAuth.Bearer
	if bearer == "" {
		bearer = policy.UpstreamBearer // Backwards compatibility.
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	} else if policy.UpstreamAuth.BasicUser != "" || policy.UpstreamAuth.BasicPass != "" {
		req.SetBasicAuth(policy.UpstreamAuth.BasicUser, policy.UpstreamAuth.BasicPass)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		writeError(w, 502, "upstream_error", "upstream request failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Never expose an upstream response body; it may contain credentials,
		// stack traces, or internal service topology.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, g.cfg.MaxBodyBytes))
		writeError(w, resp.StatusCode, "upstream_rejected", "upstream rejected the request")
		return
	}
	for _, h := range []string{"Content-Type", "Retry-After"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, io.LimitReader(resp.Body, g.cfg.MaxBodyBytes))
}

func (g *Gate) sign(c claims) (string, error) {
	b, e := json.Marshal(c)
	if e != nil {
		return "", e
	}
	p := base64.RawURLEncoding.EncodeToString(b)
	m := hmac.New(sha256.New, []byte(g.cfg.SigningKey))
	m.Write([]byte(p))
	return p + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil)), nil
}
func (g *Gate) verify(token string) (claims, error) {
	var c claims
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return c, errors.New("malformed bearer token")
	}
	m := hmac.New(sha256.New, []byte(g.cfg.SigningKey))
	m.Write([]byte(parts[0]))
	sig, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil || !hmac.Equal(sig, m.Sum(nil)) {
		return c, errors.New("invalid bearer token signature")
	}
	b, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil || json.Unmarshal(b, &c) != nil {
		return c, errors.New("invalid bearer token claims")
	}
	if g.cfg.Now().Unix() >= c.Exp {
		return c, errors.New("bearer token expired")
	}
	return c, nil
}

type memoryReplayStore struct {
	mu   sync.Mutex
	used map[string]time.Time
	now  func() time.Time
}

func newMemoryReplayStore(now func() time.Time) *memoryReplayStore {
	return &memoryReplayStore{used: make(map[string]time.Time), now: now}
}

func (s *memoryReplayStore) Consume(_ context.Context, id string, exp time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for key, expiry := range s.used {
		if !now.Before(expiry) {
			delete(s.used, key)
		}
	}
	if _, exists := s.used[id]; exists {
		return false, nil
	}
	s.used[id] = exp
	return true, nil
}

func validUpstreamHeader(name string) bool {
	name = http.CanonicalHeaderKey(name)
	switch name {
	case "Authorization", "Connection", "Content-Length", "Host", "Transfer-Encoding":
		return false
	}
	return name != ""
}
func validPath(path, prefix string) bool {
	return strings.HasPrefix(path, "/") && !strings.Contains(path, "..") && (path == prefix || strings.HasPrefix(path, strings.TrimSuffix(prefix, "/")+"/"))
}
func contains(xs []string, x string) bool {
	for _, v := range xs {
		if strings.ToUpper(v) == x {
			return true
		}
	}
	return false
}
func decodeStrict(r io.Reader, v any) error {
	d := json.NewDecoder(r)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return errors.New("body must contain one JSON value")
	}
	return nil
}
func writeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": code, "message": msg})
}
