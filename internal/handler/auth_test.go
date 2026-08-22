package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"latere.ai/x/wallfacer/internal/oauth"
	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store/storetest"
)

func newAuthTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	storeDir, err := os.MkdirTemp("", "wallfacer-auth-test-*")
	if err != nil {
		t.Fatal(err)
	}
	s, err := storetest.NewFileStore(t, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(storeDir) })
	t.Cleanup(s.WaitCompaction)

	ws := t.TempDir()
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	r := runner.NewRunner(s, runner.RunnerConfig{
		EnvFile:    envPath,
		Workspaces: []string{ws},
	})
	t.Cleanup(r.WaitBackground)
	t.Cleanup(r.Shutdown)

	h := NewHandler(s, r, t.TempDir(), []string{ws}, nil)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/{provider}/start", h.StartOAuth)
	mux.HandleFunc("GET /api/auth/{provider}/status", h.OAuthStatus)
	mux.HandleFunc("POST /api/auth/{provider}/cancel", h.CancelOAuth)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	// Starting a flow binds that provider's fixed callback port (53692 for
	// Claude, 1455 for Codex - both pinned by the redirect URI registered with
	// the provider). Nothing else releases it: each test builds its own Manager,
	// so the next one's Start finds an empty flows map and has no predecessor to
	// cancel. Without this the port stays bound for the flow's full five-minute
	// timeout and every later bind fails.
	t.Cleanup(func() {
		h.oauthManager.Cancel(oauth.ClaudeProvider.Name)
		h.oauthManager.Cancel(oauth.CodexProvider.Name)
	})
	return srv
}

func TestStartOAuth_UnknownProvider(t *testing.T) {
	srv := newAuthTestServer(t)

	resp, err := http.Post(srv.URL+"/api/auth/llama/start", "", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d; want 400", resp.StatusCode)
	}
}

func TestStartOAuth_ReturnsAuthorizeURL(t *testing.T) {
	srv := newAuthTestServer(t)

	resp, err := http.Post(srv.URL+"/api/auth/claude/start", "", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d; want 200 (body: %s)", resp.StatusCode, body)
	}

	var result struct {
		AuthorizeURL string `json:"authorize_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.AuthorizeURL == "" {
		t.Error("authorize_url is empty")
	}
	if !strings.Contains(result.AuthorizeURL, "claude.ai") {
		t.Errorf("authorize_url = %q; want to contain 'claude.ai'", result.AuthorizeURL)
	}
}

func TestOAuthStatus_NoActiveFlow(t *testing.T) {
	srv := newAuthTestServer(t)

	resp, err := http.Get(srv.URL + "/api/auth/claude/status")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var status struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if status.State != "error" {
		t.Errorf("state = %q; want 'error' (no active flow)", status.State)
	}
}

func TestCancelOAuth_NoActiveFlow(t *testing.T) {
	srv := newAuthTestServer(t)

	resp, err := http.Post(srv.URL+"/api/auth/claude/cancel", "", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d; want 204", resp.StatusCode)
	}
}

func TestStartOAuth_Integration(t *testing.T) {
	srv := newAuthTestServer(t)

	// Start flow.
	resp, err := http.Post(srv.URL+"/api/auth/codex/start", "", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("start status = %d", resp.StatusCode)
	}

	// Check status — should be pending.
	resp, err = http.Get(srv.URL + "/api/auth/codex/status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var status struct {
		State string `json:"state"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&status)
	_ = resp.Body.Close()
	if status.State != "pending" {
		t.Errorf("state = %q; want 'pending'", status.State)
	}

	// Cancel.
	resp, err = http.Post(srv.URL+"/api/auth/codex/cancel", "", nil)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Errorf("cancel status = %d; want 204", resp.StatusCode)
	}

	// Status after cancel — should be error (no active flow).
	resp, err = http.Get(srv.URL + "/api/auth/codex/status")
	if err != nil {
		t.Fatalf("status after cancel: %v", err)
	}
	_ = json.NewDecoder(resp.Body).Decode(&status)
	_ = resp.Body.Close()
	if status.State != "error" {
		t.Errorf("state after cancel = %q; want 'error'", status.State)
	}
}

// Claude's callback port is pinned to 53692 by its registered redirect URI, so
// a leaked flow makes every later authorization fail with a 500. Repeating the
// start across fresh servers proves the teardown actually frees it.
func TestStartOAuth_FixedPortIsReusableAcrossServers(t *testing.T) {
	for i := range 3 {
		t.Run(fmt.Sprintf("attempt-%d", i), func(t *testing.T) {
			srv := newAuthTestServer(t)
			resp, err := http.Post(srv.URL+"/api/auth/claude/start", "", nil)
			if err != nil {
				t.Fatalf("POST: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d; want 200 (body: %s)", resp.StatusCode, body)
			}
		})
	}
}
