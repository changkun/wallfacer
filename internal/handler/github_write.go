package handler

import (
	"net/http"
	"strings"

	"latere.ai/x/wallfacer/internal/github"
	"latere.ai/x/wallfacer/internal/pkg/httpjson"
)

// GitHub write surface (spec: github-integration component 4):
//
//	POST /api/github/pulls     create a pull request (existing-PR detection)
//	POST /api/github/comments  comment on a PR or issue
//
// Both require a connected token and the selected repo as "owner/name". PR
// title/body are taken from the request; sandbox auto-generation is a follow-up.

// splitRepo parses an "owner/name" body field, writing 400 on malformed input.
func splitRepo(w http.ResponseWriter, repo string) (owner, name string, ok bool) {
	o, n, found := strings.Cut(strings.TrimSpace(repo), "/")
	if !found || o == "" || n == "" {
		http.Error(w, "repo (owner/name) is required", http.StatusBadRequest)
		return "", "", false
	}
	return o, n, true
}

type createPullRequest struct {
	Repo  string `json:"repo"`
	Base  string `json:"base"`
	Head  string `json:"head"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Draft bool   `json:"draft"`
}

// GitHubCreatePull opens a pull request from head into base. The branch must
// already be pushed (use the existing git push controls); if a PR already
// exists for the branch, the open one is returned instead of an error.
func (h *Handler) GitHubCreatePull(w http.ResponseWriter, r *http.Request) {
	body, ok := httpjson.DecodeBody[createPullRequest](w, r)
	if !ok {
		return
	}
	owner, name, ok := splitRepo(w, body.Repo)
	if !ok {
		return
	}
	if body.Head == "" || body.Base == "" || body.Title == "" {
		http.Error(w, "head, base, and title are required", http.StatusBadRequest)
		return
	}
	tok, ok := h.githubToken(w, r)
	if !ok {
		return
	}
	pr, err := github.CreatePull(r.Context(), h.github.APIClient(), tok, owner, name, github.CreatePullParams{
		Title: body.Title, Body: body.Body, Head: body.Head, Base: body.Base, Draft: body.Draft,
	})
	if err != nil {
		mapGitHubAPIError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, pr)
}

type createCommentRequest struct {
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Body   string `json:"body"`
}

// GitHubCreateComment posts a conversation comment to a PR or issue.
func (h *Handler) GitHubCreateComment(w http.ResponseWriter, r *http.Request) {
	body, ok := httpjson.DecodeBody[createCommentRequest](w, r)
	if !ok {
		return
	}
	owner, name, ok := splitRepo(w, body.Repo)
	if !ok {
		return
	}
	if body.Number <= 0 || strings.TrimSpace(body.Body) == "" {
		http.Error(w, "number and body are required", http.StatusBadRequest)
		return
	}
	tok, ok := h.githubToken(w, r)
	if !ok {
		return
	}
	cm, err := github.CreateComment(r.Context(), h.github.APIClient(), tok, owner, name, body.Number, body.Body)
	if err != nil {
		mapGitHubAPIError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, cm)
}
