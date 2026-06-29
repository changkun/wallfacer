package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"latere.ai/x/wallfacer/internal/github"
)

// githubHandlerWithMock returns a Handler whose GitHub provider has a seeded
// valid token and a client pointed at mockGH, so repo/read endpoints exercise
// the full transport without ../auth.
func githubHandlerWithMock(t *testing.T, mockGH *httptest.Server) *Handler {
	t.Helper()
	store, _ := github.NewFileStore(t.TempDir())
	h := &Handler{}
	h.SetGitHub(&github.Provider{
		Store:  store,
		Client: &github.Client{BaseURL: mockGH.URL, HTTP: mockGH.Client()},
	})
	if err := store.Save(context.Background(), h.githubPrincipal(context.Background()),
		&github.Token{AccessToken: "ghu_x", Expiry: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("seed token: %v", err)
	}
	return h
}

func TestGitHubRepos_ListsInstallationRepos(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"repositories":[
			{"name":"wallfacer","full_name":"latere/wallfacer","default_branch":"main","owner":{"login":"latere"}}
		]}`))
	}))
	defer mock.Close()
	h := githubHandlerWithMock(t, mock)

	rec := httptest.NewRecorder()
	h.GitHubRepos(rec, httptest.NewRequest(http.MethodGet, "/api/github/repos", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	var resp struct {
		Repos []github.Repo `json:"repos"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Repos) != 1 || resp.Repos[0].FullName != "latere/wallfacer" {
		t.Errorf("repos = %+v", resp.Repos)
	}
}

func TestGitHubRepos_NotConnected(t *testing.T) {
	store, _ := github.NewFileStore(t.TempDir())
	h := &Handler{}
	h.SetGitHub(&github.Provider{Store: store}) // no token, no broker

	rec := httptest.NewRecorder()
	h.GitHubRepos(rec, httptest.NewRequest(http.MethodGet, "/api/github/repos", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestGitHubRepoSelect_WithinGrantResolvesIdentity(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"repositories":[
			{"name":"wallfacer","full_name":"latere/wallfacer","default_branch":"main","owner":{"login":"latere"}}
		]}`))
	}))
	defer mock.Close()
	h := githubHandlerWithMock(t, mock)

	req := httptest.NewRequest(http.MethodPost, "/api/github/repo/select",
		strings.NewReader(`{"repo":"latere/wallfacer"}`))
	rec := httptest.NewRecorder()
	h.GitHubRepoSelect(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	var resp repoSelectResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Identity != "github.com/latere/wallfacer" || resp.DefaultBranch != "main" {
		t.Errorf("select resp = %+v", resp)
	}
}

// A repo outside the install grant must be 403 (org boundary), never widened.
func TestGitHubRepoSelect_OutsideGrantIsForbidden(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"repositories":[
			{"name":"wallfacer","full_name":"latere/wallfacer","owner":{"login":"latere"}}
		]}`))
	}))
	defer mock.Close()
	h := githubHandlerWithMock(t, mock)

	req := httptest.NewRequest(http.MethodPost, "/api/github/repo/select",
		strings.NewReader(`{"repo":"evil/repo"}`))
	rec := httptest.NewRecorder()
	h.GitHubRepoSelect(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestGitHubRepoSelect_MissingRepoIsBadRequest(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer mock.Close()
	h := githubHandlerWithMock(t, mock)

	req := httptest.NewRequest(http.MethodPost, "/api/github/repo/select", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.GitHubRepoSelect(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
