package handler

import (
	"net/http"
	"strconv"
	"strings"

	"latere.ai/x/wallfacer/internal/github"
	"latere.ai/x/wallfacer/internal/pkg/httpjson"
)

// GitHub read surface (spec: github-integration component 3, read-surface):
//
//	GET /api/github/pulls?repo=owner/name&state=open
//	GET /api/github/pulls/{number}?repo=owner/name
//	GET /api/github/issues?repo=owner/name&state=open
//	GET /api/github/issues/{number}?repo=owner/name
//
// repo is the selected repository as "owner/name"; state filters open|closed|all.

// repoParam parses the required ?repo=owner/name query param, writing a 400 and
// returning ok=false when it is missing or malformed.
func repoParam(w http.ResponseWriter, r *http.Request) (owner, name string, ok bool) {
	full := strings.TrimSpace(r.URL.Query().Get("repo"))
	o, n, found := strings.Cut(full, "/")
	if !found || o == "" || n == "" {
		http.Error(w, "repo query param (owner/name) is required", http.StatusBadRequest)
		return "", "", false
	}
	return o, n, true
}

// numberPath parses the {number} path value, writing a 400 and returning
// ok=false when it is not a positive integer.
func numberPath(w http.ResponseWriter, r *http.Request) (int, bool) {
	n, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || n <= 0 {
		http.Error(w, "invalid number", http.StatusBadRequest)
		return 0, false
	}
	return n, true
}

// GitHubPulls lists pull requests for the selected repo.
func (h *Handler) GitHubPulls(w http.ResponseWriter, r *http.Request) {
	owner, name, ok := repoParam(w, r)
	if !ok {
		return
	}
	tok, ok := h.githubToken(w, r)
	if !ok {
		return
	}
	pulls, err := github.ListPulls(r.Context(), h.github.APIClient(), tok, owner, name, r.URL.Query().Get("state"))
	if err != nil {
		mapGitHubAPIError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"pulls": pulls})
}

// GitHubPull returns a pull request and its comment thread.
func (h *Handler) GitHubPull(w http.ResponseWriter, r *http.Request) {
	owner, name, ok := repoParam(w, r)
	if !ok {
		return
	}
	number, ok := numberPath(w, r)
	if !ok {
		return
	}
	tok, ok := h.githubToken(w, r)
	if !ok {
		return
	}
	detail, err := github.GetPull(r.Context(), h.github.APIClient(), tok, owner, name, number)
	if err != nil {
		mapGitHubAPIError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, detail)
}

// GitHubIssues lists issues for the selected repo (pull requests excluded).
func (h *Handler) GitHubIssues(w http.ResponseWriter, r *http.Request) {
	owner, name, ok := repoParam(w, r)
	if !ok {
		return
	}
	tok, ok := h.githubToken(w, r)
	if !ok {
		return
	}
	issues, err := github.ListIssues(r.Context(), h.github.APIClient(), tok, owner, name, r.URL.Query().Get("state"))
	if err != nil {
		mapGitHubAPIError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"issues": issues})
}

// GitHubIssue returns an issue and its comment thread.
func (h *Handler) GitHubIssue(w http.ResponseWriter, r *http.Request) {
	owner, name, ok := repoParam(w, r)
	if !ok {
		return
	}
	number, ok := numberPath(w, r)
	if !ok {
		return
	}
	tok, ok := h.githubToken(w, r)
	if !ok {
		return
	}
	detail, err := github.GetIssue(r.Context(), h.github.APIClient(), tok, owner, name, number)
	if err != nil {
		mapGitHubAPIError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, detail)
}
