package handler

import (
	"errors"
	"net/http"

	"latere.ai/x/wallfacer/internal/github"
)

// Shared helpers for the GitHub write surface (spec: github-integration
// component 4). The standalone repo/PR/issue browse handlers were removed in the
// task-centric redesign; what remains is token resolution and API-error mapping,
// used by the create-PR and comment handlers.

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
