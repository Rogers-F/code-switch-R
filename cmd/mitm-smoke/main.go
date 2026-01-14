package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"time"

	"codeswitch/services"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("PASS")
}

func run() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if os.Getenv("CODE_SWITCH_SMOKE_ALLOW_REAL_HOME") == "" && !looksLikeTempHome(home) {
		return fmt.Errorf("refusing to run with HOME=%q (set HOME to a temp dir, e.g. HOME=\"$(mktemp -d)\" go run ./cmd/mitm-smoke)", home)
	}

	if err := services.InitDatabase(); err != nil {
		return fmt.Errorf("InitDatabase: %w", err)
	}
	if err := services.InitGlobalDBQueue(); err != nil {
		return fmt.Errorf("InitGlobalDBQueue: %w", err)
	}
	defer services.ShutdownGlobalDBQueue(2 * time.Second)

	providerService := services.NewProviderService()
	ruleService, err := services.NewRuleService()
	if err != nil {
		return fmt.Errorf("NewRuleService: %w", err)
	}

	if err := providerService.SaveProviders("codex", []services.Provider{
		{
			ID:      1,
			Name:    "smoke-upstream",
			APIURL:  "https://upstream.example.com/openai",
			APIKey:  "testkey",
			Enabled: true,
		},
	}); err != nil {
		return fmt.Errorf("SaveProviders(codex): %w", err)
	}

	if err := providerService.SaveProviders("claude", []services.Provider{
		{
			ID:          1,
			Name:        "smoke-upstream-claude",
			APIURL:      "https://upstream.example.com/anthropic",
			APIKey:      "testkey-claude",
			Enabled:     true,
			APIEndpoint: "/v1/messages",
		},
	}); err != nil {
		return fmt.Errorf("SaveProviders(claude): %w", err)
	}

	openaiRule := &services.MITMRule{
		Name:           "smoke-rule",
		Enabled:        true,
		SourceHost:     "api.openai.com",
		TargetProvider: "codex:1",
		Priority:       100,
	}
	if err := ruleService.Create(openaiRule); err != nil {
		return fmt.Errorf("CreateRule(openai): %w", err)
	}

	claudeRule := &services.MITMRule{
		Name:           "smoke-rule-claude",
		Enabled:        true,
		SourceHost:     "api.anthropic.com",
		TargetProvider: "claude:1",
		Priority:       100,
	}
	if err := ruleService.Create(claudeRule); err != nil {
		return fmt.Errorf("CreateRule(claude): %w", err)
	}

	engine := services.NewMITMRuleEngine(ruleService, providerService)
	transport := &recordingTransport{}
	engine.SetTransport(transport)
	handler := engine.CreateRuleBasedProxy(make(chan services.MITMLogEntry, 16))

	if err := testRuleMatchSNI(handler, transport); err != nil {
		return err
	}
	if err := testDefaultAuthHeader(handler, transport); err != nil {
		return err
	}
	if err := testNoRule(handler, transport); err != nil {
		return err
	}

	return nil
}

func looksLikeTempHome(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasPrefix(path, "/tmp/") || strings.HasPrefix(path, "/var/folders/")
}

type recordedRequest struct {
	scheme string
	host   string
	path   string
	header http.Header
}

type recordingTransport struct {
	mu       sync.Mutex
	requests []recordedRequest
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.requests = append(t.requests, recordedRequest{
		scheme: req.URL.Scheme,
		host:   req.URL.Host,
		path:   req.URL.Path,
		header: req.Header.Clone(),
	})
	t.mu.Unlock()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		Request:    req,
	}
	resp.Header.Set("Content-Type", "application/json")
	return resp, nil
}

func (t *recordingTransport) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.requests)
}

func (t *recordingTransport) Last() (recordedRequest, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.requests) == 0 {
		return recordedRequest{}, false
	}
	return t.requests[len(t.requests)-1], true
}

func serve(handler http.Handler, req *http.Request) (status int, body string, err error) {
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	res := rr.Result()
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, "", err
	}
	return res.StatusCode, string(b), nil
}

func testRuleMatchSNI(handler http.Handler, transport *recordingTransport) error {
	before := transport.Count()

	req := httptest.NewRequest(http.MethodGet, "https://api.unknown.invalid/v1/models", nil)
	req.Host = "api.unknown.invalid:443"
	req.TLS = &tls.ConnectionState{ServerName: "api.openai.com"}

	status, body, err := serve(handler, req)
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("rule match (SNI priority): got status=%d, want=%d, body=%s", status, http.StatusOK, body)
	}

	last, ok := transport.Last()
	if !ok || transport.Count() != before+1 {
		return fmt.Errorf("rule match (SNI priority): expected exactly 1 upstream request, got %d", transport.Count()-before)
	}

	if last.scheme != "https" {
		return fmt.Errorf("rule match (SNI priority): upstream scheme mismatch: got %q, want %q", last.scheme, "https")
	}
	if last.host != "upstream.example.com" {
		return fmt.Errorf("rule match (SNI priority): upstream host mismatch: got %q, want %q", last.host, "upstream.example.com")
	}
	if last.path != "/openai/v1/models" {
		return fmt.Errorf("rule match (SNI priority): upstream path mismatch: got %q, want %q", last.path, "/openai/v1/models")
	}
	if got := last.header.Get("Authorization"); got != "Bearer testkey" {
		return fmt.Errorf("rule match (SNI priority): upstream auth header mismatch: got %q, want %q", got, "Bearer testkey")
	}

	return nil
}

func testDefaultAuthHeader(handler http.Handler, transport *recordingTransport) error {
	before := transport.Count()

	req := httptest.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", strings.NewReader(`{"model":"claude-3-5-sonnet","max_tokens":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.TLS = &tls.ConnectionState{ServerName: "api.anthropic.com"}

	status, body, err := serve(handler, req)
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("default auth header: got status=%d, want=%d, body=%s", status, http.StatusOK, body)
	}

	last, ok := transport.Last()
	if !ok || transport.Count() != before+1 {
		return fmt.Errorf("default auth header: expected exactly 1 upstream request, got %d", transport.Count()-before)
	}

	if last.host != "upstream.example.com" {
		return fmt.Errorf("default auth header: upstream host mismatch: got %q, want %q", last.host, "upstream.example.com")
	}
	if last.path != "/anthropic/v1/messages" {
		return fmt.Errorf("default auth header: upstream path mismatch: got %q, want %q", last.path, "/anthropic/v1/messages")
	}
	if got := last.header.Get("x-api-key"); got != "testkey-claude" {
		return fmt.Errorf("default auth header: upstream x-api-key mismatch: got %q, want %q", got, "testkey-claude")
	}
	if got := last.header.Get("Authorization"); got != "" {
		return fmt.Errorf("default auth header: unexpected Authorization header: %q", got)
	}

	return nil
}

func testNoRule(handler http.Handler, transport *recordingTransport) error {
	before := transport.Count()

	req := httptest.NewRequest(http.MethodGet, "https://api.unknown.invalid/v1/models", nil)
	req.TLS = &tls.ConnectionState{ServerName: "api.unknown.invalid"}

	status, body, err := serve(handler, req)
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	if status != http.StatusBadGateway {
		return fmt.Errorf("no rule: got status=%d, want=%d, body=%s", status, http.StatusBadGateway, body)
	}
	if transport.Count() != before {
		return fmt.Errorf("no rule: expected no upstream request, got %d", transport.Count()-before)
	}

	return nil
}
