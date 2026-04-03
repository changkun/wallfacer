package oauth

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

var testProvider = Provider{
	Name:         "test",
	AuthorizeURL: "https://example.com/authorize",
	TokenURL:     "", // overridden per test
	ClientID:     "test-client-id",
	Scopes:       []string{"read", "write"},
	TokenEnvKey:  "TEST_TOKEN",
}

func TestManager_StartVerifierError(t *testing.T) {
	old := randReader
	randReader = failReader{}
	defer func() { randReader = old }()

	m := NewManager()
	_, err := m.Start(context.Background(), testProvider)
	if err == nil {
		t.Fatal("expected error when verifier generation fails")
	}
	if !strings.Contains(err.Error(), "generate verifier") {
		t.Errorf("err = %v; want contains 'generate verifier'", err)
	}
}

// partialReader succeeds for the first n calls then fails.
type partialReader struct {
	remaining int
}

func (r *partialReader) Read(b []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, fmt.Errorf("rand failure")
	}
	r.remaining--
	return rand.Reader.Read(b)
}

func TestManager_StartStateError(t *testing.T) {
	// Allow verifier (1 call) but fail on state (2nd call).
	old := randReader
	randReader = &partialReader{remaining: 1}
	defer func() { randReader = old }()

	m := NewManager()
	_, err := m.Start(context.Background(), testProvider)
	if err == nil {
		t.Fatal("expected error when state generation fails")
	}
	if !strings.Contains(err.Error(), "generate state") {
		t.Errorf("err = %v; want contains 'generate state'", err)
	}
}

func TestManager_StartBuildsCorrectURL(t *testing.T) {
	m := NewManager()
	defer m.Cancel("test")

	authURL, err := m.Start(context.Background(), testProvider)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}

	if u.Scheme != "https" || u.Host != "example.com" || u.Path != "/authorize" {
		t.Errorf("unexpected base URL: %s", authURL)
	}

	q := u.Query()
	if q.Get("client_id") != "test-client-id" {
		t.Errorf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q", q.Get("response_type"))
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q", q.Get("code_challenge_method"))
	}
	if q.Get("code_challenge") == "" {
		t.Error("code_challenge is empty")
	}
	if q.Get("state") == "" {
		t.Error("state is empty")
	}
	if q.Get("scope") != "read write" {
		t.Errorf("scope = %q; want 'read write'", q.Get("scope"))
	}
	if q.Get("redirect_uri") == "" {
		t.Error("redirect_uri is empty")
	}
}

func TestManager_StartCancelsPreviousFlow(t *testing.T) {
	m := NewManager()

	_, err := m.Start(context.Background(), testProvider)
	if err != nil {
		t.Fatalf("Start 1: %v", err)
	}

	// Start a second flow — should cancel the first.
	_, err = m.Start(context.Background(), testProvider)
	if err != nil {
		t.Fatalf("Start 2: %v", err)
	}

	m.Cancel("test")
}

func TestManager_StatusPending(t *testing.T) {
	m := NewManager()
	defer m.Cancel("test")

	_, err := m.Start(context.Background(), testProvider)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	status := m.Status("test")
	if status.State != FlowPending {
		t.Errorf("State = %q; want %q", status.State, FlowPending)
	}
}

func TestManager_StatusNoFlow(t *testing.T) {
	m := NewManager()
	status := m.Status("nonexistent")
	if status.State != FlowError {
		t.Errorf("State = %q; want %q", status.State, FlowError)
	}
}

func TestManager_Cancel(t *testing.T) {
	m := NewManager()

	_, err := m.Start(context.Background(), testProvider)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	m.Cancel("test")

	// After cancel, status should be error (no active flow).
	status := m.Status("test")
	if status.State != FlowError {
		t.Errorf("State = %q; want %q", status.State, FlowError)
	}
}

func TestExchangeToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q; want POST", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if r.FormValue("grant_type") != "authorization_code" {
			t.Errorf("grant_type = %q", r.FormValue("grant_type"))
		}
		if r.FormValue("code") != "test-code" {
			t.Errorf("code = %q", r.FormValue("code"))
		}
		if r.FormValue("code_verifier") != "test-verifier" {
			t.Errorf("code_verifier = %q", r.FormValue("code_verifier"))
		}
		if r.FormValue("client_id") != "test-client-id" {
			t.Errorf("client_id = %q", r.FormValue("client_id"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "tok_abc123"})
	}))
	defer ts.Close()

	p := testProvider
	p.TokenURL = ts.URL

	token, err := exchangeToken(http.DefaultClient, p, "test-code", "test-verifier", "http://localhost:9999/callback", "test-state")
	if err != nil {
		t.Fatalf("exchangeToken: %v", err)
	}
	if token != "tok_abc123" {
		t.Errorf("token = %q; want %q", token, "tok_abc123")
	}
}

func TestExchangeToken_ErrorResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
	}))
	defer ts.Close()

	p := testProvider
	p.TokenURL = ts.URL

	_, err := exchangeToken(http.DefaultClient, p, "bad-code", "verifier", "http://localhost:9999/callback", "state")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestManager_StartNoScopes(t *testing.T) {
	m := NewManager()
	p := testProvider
	p.Scopes = nil
	defer m.Cancel(p.Name)

	authURL, err := m.Start(context.Background(), p)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	u, _ := url.Parse(authURL)
	if u.Query().Get("scope") != "" {
		t.Errorf("scope = %q; want empty for nil Scopes", u.Query().Get("scope"))
	}
}

func TestManager_StartCustomCallbackPath(t *testing.T) {
	m := NewManager()
	p := testProvider
	p.CallbackPath = "/auth/callback"
	defer m.Cancel(p.Name)

	authURL, err := m.Start(context.Background(), p)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	u, _ := url.Parse(authURL)
	redirectURI := u.Query().Get("redirect_uri")
	ru, _ := url.Parse(redirectURI)
	if ru.Path != "/auth/callback" {
		t.Errorf("redirect_uri path = %q; want /auth/callback", ru.Path)
	}
}

func TestManager_FlowOAuthError(t *testing.T) {
	p := testProvider
	p.TokenURL = "http://localhost:1/unused"

	m := NewManager()
	authURL, err := m.Start(context.Background(), p)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	u, _ := url.Parse(authURL)
	redirectURI := u.Query().Get("redirect_uri")
	ru, _ := url.Parse(redirectURI)

	// Send an error callback.
	callbackURL := fmt.Sprintf("http://127.0.0.1:%s/callback?error=access_denied&error_description=User+denied", ru.Port())
	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("callback GET: %v", err)
	}
	_ = resp.Body.Close()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for flow error")
		default:
		}
		status := m.Status(p.Name)
		if status.State == FlowError {
			if status.Error != "access_denied: User denied" {
				t.Errorf("error = %q; want 'access_denied: User denied'", status.Error)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestManager_FlowStateMismatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "tok"})
	}))
	defer ts.Close()

	p := testProvider
	p.TokenURL = ts.URL

	m := NewManager()
	authURL, err := m.Start(context.Background(), p)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	u, _ := url.Parse(authURL)
	redirectURI := u.Query().Get("redirect_uri")
	ru, _ := url.Parse(redirectURI)

	// Send callback with wrong state.
	callbackURL := fmt.Sprintf("http://127.0.0.1:%s/callback?code=auth-code&state=wrong-state", ru.Port())
	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("callback GET: %v", err)
	}
	_ = resp.Body.Close()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for state mismatch error")
		default:
		}
		status := m.Status(p.Name)
		if status.State == FlowError {
			if status.Error != "state mismatch" {
				t.Errorf("error = %q; want 'state mismatch'", status.Error)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestManager_FlowTokenExchangeError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer ts.Close()

	p := testProvider
	p.TokenURL = ts.URL

	m := NewManager()
	authURL, err := m.Start(context.Background(), p)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	u, _ := url.Parse(authURL)
	state := u.Query().Get("state")
	redirectURI := u.Query().Get("redirect_uri")
	ru, _ := url.Parse(redirectURI)

	callbackURL := fmt.Sprintf("http://127.0.0.1:%s/callback?code=auth-code&state=%s", ru.Port(), state)
	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("callback GET: %v", err)
	}
	_ = resp.Body.Close()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for token exchange error")
		default:
		}
		status := m.Status(p.Name)
		if status.State == FlowError {
			if !strings.Contains(status.Error, "token exchange failed") {
				t.Errorf("error = %q; want contains 'token exchange failed'", status.Error)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestManager_FlowTokenWriterError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "tok"})
	}))
	defer ts.Close()

	p := testProvider
	p.TokenURL = ts.URL

	m := NewManager()
	m.TokenWriter = func(_, _ string) error {
		return fmt.Errorf("disk full")
	}

	authURL, err := m.Start(context.Background(), p)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	u, _ := url.Parse(authURL)
	state := u.Query().Get("state")
	redirectURI := u.Query().Get("redirect_uri")
	ru, _ := url.Parse(redirectURI)

	callbackURL := fmt.Sprintf("http://127.0.0.1:%s/callback?code=auth-code&state=%s", ru.Port(), state)
	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("callback GET: %v", err)
	}
	_ = resp.Body.Close()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for token writer error")
		default:
		}
		status := m.Status(p.Name)
		if status.State == FlowError {
			if !strings.Contains(status.Error, "failed to save token") {
				t.Errorf("error = %q; want contains 'failed to save token'", status.Error)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestManager_FlowCallbackTimeout(t *testing.T) {
	p := testProvider
	p.TokenURL = "http://localhost:1/unused"

	m := NewManager()

	// Start flow then cancel immediately to trigger callback timeout path.
	_, err := m.Start(context.Background(), p)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Cancel triggers context cancellation, which makes callback.Wait return error.
	m.Cancel(p.Name)

	// Flow was removed by Cancel, status should show no active flow.
	status := m.Status(p.Name)
	if status.State != FlowError {
		t.Errorf("State = %q; want %q", status.State, FlowError)
	}
}

func TestExchangeToken_JSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q; want POST", r.Method)
		}
		ct := r.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("Content-Type = %q; want application/json", ct)
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["grant_type"] != "authorization_code" {
			t.Errorf("grant_type = %q", body["grant_type"])
		}
		if body["code"] != "test-code" {
			t.Errorf("code = %q", body["code"])
		}
		if body["state"] != "test-state" {
			t.Errorf("state = %q", body["state"])
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "json-tok"})
	}))
	defer ts.Close()

	p := testProvider
	p.TokenURL = ts.URL
	p.JSONTokenReq = true

	token, err := exchangeToken(http.DefaultClient, p, "test-code", "test-verifier", "http://localhost:9999/callback", "test-state")
	if err != nil {
		t.Fatalf("exchangeToken: %v", err)
	}
	if token != "json-tok" {
		t.Errorf("token = %q; want %q", token, "json-tok")
	}
}

func TestExchangeToken_JSONEmptyState(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if _, ok := body["state"]; ok {
			t.Errorf("state should not be present when empty")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "tok"})
	}))
	defer ts.Close()

	p := testProvider
	p.TokenURL = ts.URL
	p.JSONTokenReq = true

	_, err := exchangeToken(http.DefaultClient, p, "code", "verifier", "http://localhost:9999/cb", "")
	if err != nil {
		t.Fatalf("exchangeToken: %v", err)
	}
}

func TestExchangeToken_APIKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"api_key": "sk-abc123"})
	}))
	defer ts.Close()

	p := testProvider
	p.TokenURL = ts.URL

	token, err := exchangeToken(http.DefaultClient, p, "code", "verifier", "http://localhost:9999/callback", "state")
	if err != nil {
		t.Fatalf("exchangeToken: %v", err)
	}
	if token != "sk-abc123" {
		t.Errorf("token = %q; want %q", token, "sk-abc123")
	}
}

func TestExchangeToken_NoToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"something_else": "value"})
	}))
	defer ts.Close()

	p := testProvider
	p.TokenURL = ts.URL

	_, err := exchangeToken(http.DefaultClient, p, "code", "verifier", "http://localhost:9999/callback", "state")
	if err == nil {
		t.Fatal("expected error for missing token")
	}
	if !strings.Contains(err.Error(), "no access_token or api_key") {
		t.Errorf("err = %v; want contains 'no access_token or api_key'", err)
	}
}

func TestExchangeToken_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	}))
	defer ts.Close()

	p := testProvider
	p.TokenURL = ts.URL

	_, err := exchangeToken(http.DefaultClient, p, "code", "verifier", "http://localhost:9999/callback", "state")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "parse token response") {
		t.Errorf("err = %v; want contains 'parse token response'", err)
	}
}

func TestExchangeToken_NetworkError(t *testing.T) {
	p := testProvider
	p.TokenURL = "http://127.0.0.1:1/token" // nothing listening

	_, err := exchangeToken(http.DefaultClient, p, "code", "verifier", "http://localhost:9999/callback", "state")
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

func TestManager_StartCallbackServerError(t *testing.T) {
	// Use a port that's already bound to force NewCallbackServer to fail.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	port := ln.Addr().(*net.TCPAddr).Port

	p := testProvider
	p.FixedPort = port

	m := NewManager()
	_, err = m.Start(context.Background(), p)
	if err == nil {
		t.Fatal("expected error when callback server can't bind")
	}
	if !strings.Contains(err.Error(), "start callback server") {
		t.Errorf("err = %v; want contains 'start callback server'", err)
	}
}

type errReadCloser struct{}

func (errReadCloser) Read([]byte) (int, error) { return 0, fmt.Errorf("read error") }
func (errReadCloser) Close() error             { return nil }

func TestExchangeToken_ReadBodyError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Create a client with a transport that replaces the response body with one that errors.
	client := &http.Client{
		Transport: &bodyErrTransport{wrapped: http.DefaultTransport},
	}

	p := testProvider
	p.TokenURL = ts.URL

	_, err := exchangeToken(client, p, "code", "verifier", "http://localhost:9999/cb", "state")
	if err == nil {
		t.Fatal("expected error from body read failure")
	}
	if !strings.Contains(err.Error(), "read response") {
		t.Errorf("err = %v; want contains 'read response'", err)
	}
}

type bodyErrTransport struct {
	wrapped http.RoundTripper
}

func (t *bodyErrTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.wrapped.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	_ = resp.Body.Close()
	resp.Body = errReadCloser{}
	return resp, nil
}

func TestManager_FullFlow(t *testing.T) {
	// Mock token endpoint.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "full-flow-token"})
	}))
	defer ts.Close()

	p := testProvider
	p.TokenURL = ts.URL

	var writtenKey, writtenToken string
	m := NewManager()
	m.TokenWriter = func(envKey, token string) error {
		writtenKey = envKey
		writtenToken = token
		return nil
	}

	authURL, err := m.Start(context.Background(), p)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Extract state and callback port from the authorize URL.
	u, _ := url.Parse(authURL)
	state := u.Query().Get("state")
	redirectURI := u.Query().Get("redirect_uri")
	ru, _ := url.Parse(redirectURI)

	// Send the callback.
	callbackURL := fmt.Sprintf("http://127.0.0.1:%s/callback?code=auth-code-123&state=%s", ru.Port(), state)
	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("callback GET: %v", err)
	}
	_ = resp.Body.Close()

	// Poll until success or timeout.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for flow success; last status: %+v", m.Status(p.Name))
		default:
		}
		status := m.Status(p.Name)
		if status.State == FlowSuccess {
			break
		}
		if status.State == FlowError {
			t.Fatalf("flow failed: %s", status.Error)
		}
		time.Sleep(50 * time.Millisecond)
	}

	if writtenKey != "TEST_TOKEN" {
		t.Errorf("writtenKey = %q; want %q", writtenKey, "TEST_TOKEN")
	}
	if writtenToken != "full-flow-token" {
		t.Errorf("writtenToken = %q; want %q", writtenToken, "full-flow-token")
	}
}
