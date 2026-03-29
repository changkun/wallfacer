package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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

	token, err := exchangeToken(http.DefaultClient, p, "test-code", "test-verifier", "http://localhost:9999/callback")
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

	_, err := exchangeToken(http.DefaultClient, p, "bad-code", "verifier", "http://localhost:9999/callback")
	if err == nil {
		t.Fatal("expected error")
	}
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
