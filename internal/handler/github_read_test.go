package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"latere.ai/x/wallfacer/internal/github"
)

func TestGitHubPulls_ListsForRepo(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/repos/latere/wallfacer/pulls") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"number":42,"title":"T","state":"open","user":{"login":"o"}}]`))
	}))
	defer mock.Close()
	h := githubHandlerWithMock(t, mock)

	rec := httptest.NewRecorder()
	h.GitHubPulls(rec, httptest.NewRequest(http.MethodGet, "/api/github/pulls?repo=latere/wallfacer&state=open", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body)
	}
	var resp struct {
		Pulls []github.PullRequest `json:"pulls"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Pulls) != 1 || resp.Pulls[0].Number != 42 {
		t.Errorf("pulls = %+v", resp.Pulls)
	}
}

func TestGitHubPulls_MissingRepoParamIs400(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer mock.Close()
	h := githubHandlerWithMock(t, mock)

	rec := httptest.NewRecorder()
	h.GitHubPulls(rec, httptest.NewRequest(http.MethodGet, "/api/github/pulls", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestGitHubPull_DetailWithNumber(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/comments") {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_, _ = w.Write([]byte(`{"number":42,"title":"T","state":"open","user":{"login":"o"}}`))
	}))
	defer mock.Close()
	h := githubHandlerWithMock(t, mock)

	req := httptest.NewRequest(http.MethodGet, "/api/github/pulls/42?repo=latere/wallfacer", nil)
	req.SetPathValue("number", "42")
	rec := httptest.NewRecorder()
	h.GitHubPull(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body)
	}
	var detail github.PullRequestDetail
	_ = json.Unmarshal(rec.Body.Bytes(), &detail)
	if detail.Number != 42 {
		t.Errorf("detail = %+v", detail)
	}
}

func TestGitHubPull_BadNumberIs400(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer mock.Close()
	h := githubHandlerWithMock(t, mock)

	req := httptest.NewRequest(http.MethodGet, "/api/github/pulls/x?repo=o/r", nil)
	req.SetPathValue("number", "x")
	rec := httptest.NewRecorder()
	h.GitHubPull(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestGitHubIssues_ListsForRepo(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"number":7,"title":"I","state":"open","user":{"login":"a"}}]`))
	}))
	defer mock.Close()
	h := githubHandlerWithMock(t, mock)

	rec := httptest.NewRecorder()
	h.GitHubIssues(rec, httptest.NewRequest(http.MethodGet, "/api/github/issues?repo=o/r", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body)
	}
	var resp struct {
		Issues []github.Issue `json:"issues"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Issues) != 1 || resp.Issues[0].Number != 7 {
		t.Errorf("issues = %+v", resp.Issues)
	}
}
