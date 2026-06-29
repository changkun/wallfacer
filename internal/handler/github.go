package handler

import (
	"errors"
	"net/http"

	"latere.ai/x/wallfacer/internal/github"
	"latere.ai/x/wallfacer/internal/pkg/httpjson"
)

// GitHub repo surface (spec: github-integration component 2, repo-selection):
//
//	GET  /api/github/repos        -> repos the "Latere AI" install grants
//	POST /api/github/repo/select  -> validate a choice, resolve to host/owner/repo
//
// Both require a connected token (resolved via the provider) and operate over
// installation repos, so the install grant is the org boundary.

// githubToken resolves a usable token for the request principal, writing the
// appropriate error response and returning ok=false when none is available.
// 401 -> the UI prompts to connect; 503 -> the surface is not wired at all.
func (h *Handler) githubToken(w http.ResponseWriter, r *http.Request) (*github.Token, bool) {
	if h.github == nil {
		http.Error(w, "github not configured", http.StatusServiceUnavailable)
		return nil, false
	}
	tok, err := h.github.Get(r.Context(), h.githubPrincipal(r.Context()))
	if err != nil {
		if errors.Is(err, github.ErrNotConnected) {
			http.Error(w, "github not connected", http.StatusUnauthorized)
			return nil, false
		}
		http.Error(w, "github token unavailable", http.StatusBadGateway)
		return nil, false
	}
	return tok, true
}

// mapGitHubAPIError translates an API client error into an HTTP status,
// preserving the org-boundary (403), auth (401), and rate-limit (429) signals
// the UI branches on.
func mapGitHubAPIError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, github.ErrUnauthorized):
		http.Error(w, "github unauthorized", http.StatusUnauthorized)
	case errors.Is(err, github.ErrRateLimited):
		http.Error(w, "github rate limited", http.StatusTooManyRequests)
	case errors.Is(err, github.ErrForbidden):
		http.Error(w, "github forbidden (outside organization)", http.StatusForbidden)
	case errors.Is(err, github.ErrNotFound):
		http.Error(w, "github not found", http.StatusNotFound)
	default:
		http.Error(w, "github request failed", http.StatusBadGateway)
	}
}

// GitHubRepos lists the repositories the install grants the principal.
func (h *Handler) GitHubRepos(w http.ResponseWriter, r *http.Request) {
	tok, ok := h.githubToken(w, r)
	if !ok {
		return
	}
	repos, err := github.ListInstallationRepos(r.Context(), h.github.APIClient(), tok)
	if err != nil {
		mapGitHubAPIError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"repos": repos})
}

type repoSelectRequest struct {
	// Repo is the chosen repository as "owner/name".
	Repo string `json:"repo"`
}

type repoSelectResponse struct {
	FullName      string `json:"full_name"`
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url,omitempty"`
	// Identity is the canonical host/owner/repo join key (repo-identity).
	Identity string `json:"identity"`
}

// GitHubRepoSelect validates that the chosen repo is within the install grant
// (the org boundary) and resolves it to its canonical host/owner/repo identity.
// Selection state itself is held client-side; this endpoint is the server-side
// validation + identity resolution the read/write surfaces key on.
func (h *Handler) GitHubRepoSelect(w http.ResponseWriter, r *http.Request) {
	body, ok := httpjson.DecodeBody[repoSelectRequest](w, r)
	if !ok {
		return
	}
	if body.Repo == "" {
		http.Error(w, "repo is required", http.StatusBadRequest)
		return
	}
	tok, ok := h.githubToken(w, r)
	if !ok {
		return
	}
	repos, err := github.ListInstallationRepos(r.Context(), h.github.APIClient(), tok)
	if err != nil {
		mapGitHubAPIError(w, err)
		return
	}
	for _, repo := range repos {
		if repo.FullName == body.Repo {
			httpjson.Write(w, http.StatusOK, repoSelectResponse{
				FullName:      repo.FullName,
				Owner:         repo.Owner,
				Name:          repo.Name,
				DefaultBranch: repo.DefaultBranch,
				HTMLURL:       repo.HTMLURL,
				Identity:      "github.com/" + repo.FullName,
			})
			return
		}
	}
	// Not in the install grant: outside the org boundary, never silently widened.
	http.Error(w, "repo outside your organization's installation", http.StatusForbidden)
}
