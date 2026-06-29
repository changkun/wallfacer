package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"latere.ai/x/wallfacer/internal/github"
)

func decodeStatus(t *testing.T, body []byte) githubAuthStatus {
	t.Helper()
	var st githubAuthStatus
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatalf("decode status: %v (body %s)", err, body)
	}
	return st
}

func TestGitHubAuthStatus_NotConfigured(t *testing.T) {
	h := &Handler{} // no SetGitHub
	rec := httptest.NewRecorder()
	h.GitHubAuthStatus(rec, httptest.NewRequest(http.MethodGet, "/api/github/auth/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200", rec.Code)
	}
	st := decodeStatus(t, rec.Body.Bytes())
	if st.Available || st.Connected {
		t.Errorf("unconfigured = %+v, want available=false connected=false", st)
	}
}

func TestGitHubAuthStatus_ConfiguredDisconnected(t *testing.T) {
	store, _ := github.NewFileStore(t.TempDir())
	h := &Handler{}
	h.SetGitHub(&github.Provider{Store: store})

	rec := httptest.NewRecorder()
	h.GitHubAuthStatus(rec, httptest.NewRequest(http.MethodGet, "/api/github/auth/status", nil))
	st := decodeStatus(t, rec.Body.Bytes())
	if !st.Available {
		t.Error("Available = false, want true when store is wired")
	}
	if st.Connected {
		t.Error("Connected = true with no stored token")
	}
	if st.CanConnect {
		t.Error("CanConnect = true with no broker wired")
	}
}

func TestGitHubAuthStatus_Connected(t *testing.T) {
	store, _ := github.NewFileStore(t.TempDir())
	h := &Handler{}
	h.SetGitHub(&github.Provider{Store: store})
	p := h.githubPrincipal(context.Background())
	exp := time.Now().Add(time.Hour).Round(time.Second)
	if err := store.Save(context.Background(), p, &github.Token{
		AccessToken: "ghu_live", Login: "octocat", Account: "latere",
		Permissions: []string{"contents", "pull_requests"}, Expiry: exp,
	}); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	rec := httptest.NewRecorder()
	h.GitHubAuthStatus(rec, httptest.NewRequest(http.MethodGet, "/api/github/auth/status", nil))
	st := decodeStatus(t, rec.Body.Bytes())
	if !st.Connected || st.Login != "octocat" || st.Account != "latere" {
		t.Errorf("connected status = %+v", st)
	}
	if len(st.Permissions) != 2 {
		t.Errorf("permissions = %v", st.Permissions)
	}
	if st.ExpiresAt == nil || !st.ExpiresAt.Equal(exp) {
		t.Errorf("expires_at = %v, want %v", st.ExpiresAt, exp)
	}
}

// An expired stored token must report disconnected (status must not present a
// dead credential as a live connection).
func TestGitHubAuthStatus_ExpiredTokenIsDisconnected(t *testing.T) {
	store, _ := github.NewFileStore(t.TempDir())
	h := &Handler{}
	h.SetGitHub(&github.Provider{Store: store})
	p := h.githubPrincipal(context.Background())
	_ = store.Save(context.Background(), p, &github.Token{
		AccessToken: "stale", Expiry: time.Now().Add(-time.Hour),
	})

	rec := httptest.NewRecorder()
	h.GitHubAuthStatus(rec, httptest.NewRequest(http.MethodGet, "/api/github/auth/status", nil))
	st := decodeStatus(t, rec.Body.Bytes())
	if st.Connected {
		t.Error("expired token reported as connected")
	}
}

func TestGitHubAuthConnect_UnavailableWithoutBroker(t *testing.T) {
	store, _ := github.NewFileStore(t.TempDir())
	h := &Handler{}
	h.SetGitHub(&github.Provider{Store: store}) // no broker

	rec := httptest.NewRecorder()
	h.GitHubAuthConnect(rec, httptest.NewRequest(http.MethodPost, "/api/github/auth/connect", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("connect without broker = %d, want 503", rec.Code)
	}
}

func TestGitHubAuthDisconnect_ClearsToken(t *testing.T) {
	store, _ := github.NewFileStore(t.TempDir())
	h := &Handler{}
	h.SetGitHub(&github.Provider{Store: store})
	ctx := context.Background()
	p := h.githubPrincipal(ctx)
	if err := store.Save(ctx, p, &github.Token{AccessToken: "x", Expiry: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rec := httptest.NewRecorder()
	h.GitHubAuthDisconnect(rec, httptest.NewRequest(http.MethodPost, "/api/github/auth/disconnect", nil))
	if rec.Code != http.StatusNoContent {
		t.Errorf("disconnect = %d, want 204", rec.Code)
	}
	tok, _ := store.Load(ctx, p)
	if tok != nil {
		t.Errorf("token survived disconnect: %+v", tok)
	}
}
