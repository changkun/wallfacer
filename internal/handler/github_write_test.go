package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"latere.ai/x/wallfacer/internal/github"
)

func TestGitHubCreatePull_Creates(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":7,"title":"T","state":"open","html_url":"u","user":{"login":"me"}}`))
	}))
	defer mock.Close()
	h := githubHandlerWithMock(t, mock)

	req := httptest.NewRequest(http.MethodPost, "/api/github/pulls",
		strings.NewReader(`{"repo":"o/r","base":"main","head":"feature","title":"T","body":"B"}`))
	rec := httptest.NewRecorder()
	h.GitHubCreatePull(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body)
	}
	var pr github.PullRequest
	_ = json.Unmarshal(rec.Body.Bytes(), &pr)
	if pr.Number != 7 {
		t.Errorf("pr = %+v", pr)
	}
}

func TestGitHubCreatePull_MissingFields400(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer mock.Close()
	h := githubHandlerWithMock(t, mock)

	// missing title
	req := httptest.NewRequest(http.MethodPost, "/api/github/pulls",
		strings.NewReader(`{"repo":"o/r","base":"main","head":"feature"}`))
	rec := httptest.NewRecorder()
	h.GitHubCreatePull(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestGitHubCreateComment_Posts(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/issues/42/comments") {
			t.Errorf("path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"body":"hi","user":{"login":"me"}}`))
	}))
	defer mock.Close()
	h := githubHandlerWithMock(t, mock)

	req := httptest.NewRequest(http.MethodPost, "/api/github/comments",
		strings.NewReader(`{"repo":"o/r","number":42,"body":"hi"}`))
	rec := httptest.NewRecorder()
	h.GitHubCreateComment(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body)
	}
	var cm github.Comment
	_ = json.Unmarshal(rec.Body.Bytes(), &cm)
	if cm.Body != "hi" || cm.Author != "me" {
		t.Errorf("comment = %+v", cm)
	}
}

func TestGitHubCreateComment_EmptyBody400(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer mock.Close()
	h := githubHandlerWithMock(t, mock)

	req := httptest.NewRequest(http.MethodPost, "/api/github/comments",
		strings.NewReader(`{"repo":"o/r","number":42,"body":"  "}`))
	rec := httptest.NewRecorder()
	h.GitHubCreateComment(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestGitHubCreateComment_NotConnected401(t *testing.T) {
	store, _ := github.NewFileStore(t.TempDir())
	h := &Handler{}
	h.SetGitHub(&github.Provider{Store: store}) // no token

	req := httptest.NewRequest(http.MethodPost, "/api/github/comments",
		strings.NewReader(`{"repo":"o/r","number":1,"body":"x"}`))
	rec := httptest.NewRecorder()
	h.GitHubCreateComment(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}
