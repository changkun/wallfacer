package handler

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"latere.ai/x/wallfacer/internal/auth"
	"latere.ai/x/wallfacer/internal/github"
	"latere.ai/x/wallfacer/internal/pkg/httpjson"
)

// GitHub auth surface (spec: github-integration component 1, oauth-token-store):
//
//	GET  /api/github/auth/status      -> connection state for the principal
//	POST /api/github/auth/connect     -> start the brokered install + grant flow
//	POST /api/github/auth/disconnect  -> clear the stored token
//
// Status and disconnect operate on the token store alone. Connect needs the
// ../auth broker (the "Latere AI" GitHub App is brokered there); until that is
// wired the endpoint reports the connect flow unavailable rather than 500.

// SetGitHub wires the principal-scoped GitHub token provider. Called from the
// CLI boot path; leaving it unset disables the /api/github/* surface.
func (h *Handler) SetGitHub(p *github.Provider) { h.github = p }

// SetGitHubBroker wires the live broker onto the provider, enabling connect and
// token resolution against the ../auth service. No-op if the provider is unset.
func (h *Handler) SetGitHubBroker(b github.Broker) {
	if h.github != nil {
		h.github.Broker = b
	}
}

// githubPrincipal resolves the owner key a GitHub token is scoped to. In cloud
// mode it comes from the authenticated identity; a local single-user run has no
// principal, so it falls back to a fixed local key so the token still persists
// per machine.
func (h *Handler) githubPrincipal(ctx context.Context) github.Principal {
	if c, ok := auth.PrincipalFromContext(ctx); ok && c != nil && c.Sub != "" {
		return github.Principal{Sub: c.Sub, OrgID: c.OrgID}
	}
	return github.Principal{Sub: "local"}
}

// githubAuthStatus is the connection state surfaced to the UI and mirrored into
// /api/config. available is false when the GitHub surface is not wired at all.
type githubAuthStatus struct {
	Available   bool       `json:"available"`
	Connected   bool       `json:"connected"`
	Login       string     `json:"login,omitempty"`
	Account     string     `json:"account,omitempty"`
	Permissions []string   `json:"permissions,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	// CanConnect reports whether the live connect flow can run (the ../auth
	// broker is wired). When false the UI shows connect as unavailable.
	CanConnect bool `json:"can_connect"`
}

// githubStatus computes the current status for the principal from the token
// store, without triggering a broker refresh (a status check must be cheap and
// must not mint tokens).
func (h *Handler) githubStatus(ctx context.Context) githubAuthStatus {
	if h.github == nil {
		return githubAuthStatus{}
	}
	st := githubAuthStatus{Available: true, CanConnect: h.github.Broker != nil}
	if h.github.Store == nil {
		return st
	}
	tok, err := h.github.Store.Load(ctx, h.githubPrincipal(ctx))
	if err != nil || !tok.Valid() {
		return st
	}
	st.Connected = true
	st.Login = tok.Login
	st.Account = tok.Account
	st.Permissions = tok.Permissions
	if !tok.Expiry.IsZero() {
		exp := tok.Expiry
		st.ExpiresAt = &exp
	}
	return st
}

// GitHubAuthStatus reports whether the principal has a connected GitHub App
// token, plus the login/account/permissions for the connected UI.
func (h *Handler) GitHubAuthStatus(w http.ResponseWriter, r *http.Request) {
	if h.github == nil {
		httpjson.Write(w, http.StatusOK, githubAuthStatus{})
		return
	}
	httpjson.Write(w, http.StatusOK, h.githubStatus(r.Context()))
}

// GitHubAuthConnect starts the brokered install + grant flow. Gated on the
// ../auth broker; until it is wired the endpoint reports the flow unavailable
// (503) so the UI shows a clear state rather than a server error.
func (h *Handler) GitHubAuthConnect(w http.ResponseWriter, r *http.Request) {
	if h.github == nil || h.github.Broker == nil || h.authURL == "" {
		http.Error(w, "github connect not available", http.StatusServiceUnavailable)
		return
	}
	// The brokered install + grant flow lives on the ../auth service; return its
	// install-start URL for the UI to navigate to. After installing, ../auth
	// captures the user token; wallfacer's next status poll resolves it via the
	// broker and flips to connected.
	install := strings.TrimRight(h.authURL, "/") + "/me/integrations/github/install/start"
	if ret := strings.TrimSpace(r.URL.Query().Get("return_to")); ret != "" {
		install += "?return_to=" + url.QueryEscape(ret)
	}
	httpjson.Write(w, http.StatusOK, map[string]string{"install_url": install})
}

// GitHubAuthDisconnect clears the stored token for the principal.
func (h *Handler) GitHubAuthDisconnect(w http.ResponseWriter, r *http.Request) {
	if h.github == nil || h.github.Store == nil {
		http.Error(w, "github not configured", http.StatusServiceUnavailable)
		return
	}
	if err := h.github.Store.Clear(r.Context(), h.githubPrincipal(r.Context())); err != nil {
		http.Error(w, "github disconnect failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
