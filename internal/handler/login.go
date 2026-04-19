package handler

import (
	"encoding/json"
	"net/http"

	"changkun.de/x/wallfacer/internal/auth"
)

// AuthProvider is the subset of *auth.Client the HTTP handlers need. Kept
// as an interface so tests can substitute a fake without spinning up a
// real OIDC client. An untyped-nil value means auth is not configured.
type AuthProvider interface {
	HandleLogin(http.ResponseWriter, *http.Request)
	HandleCallback(http.ResponseWriter, *http.Request)
	HandleLogout(http.ResponseWriter, *http.Request)
	UserFromRequest(http.ResponseWriter, *http.Request) *auth.User
	AuthURL() string
}

// SetAuth installs an OIDC sign-in provider. Pass nil to leave auth
// unconfigured (the default). Called from the CLI boot path when
// WALLFACER_CLOUD=true and oidc.New returns a valid client.
func (h *Handler) SetAuth(p AuthProvider) {
	h.auth = p
	if p != nil {
		h.authURL = p.AuthURL()
	}
}

// HasAuth reports whether a cloud-mode OIDC client is wired. Used by
// server-side wiring to decide whether to apply authorization wrappers
// (e.g. RequireSuperadmin) to individual routes.
func (h *Handler) HasAuth() bool { return h.auth != nil }

// Login redirects the browser to the auth service's authorize endpoint.
// Returns 503 when auth is not configured so broken deployments fail
// loudly instead of silently 404'ing.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		http.Error(w, "auth not configured", http.StatusServiceUnavailable)
		return
	}
	h.auth.HandleLogin(w, r)
}

// Callback completes the OAuth exchange and sets the session cookie.
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		http.Error(w, "auth not configured", http.StatusServiceUnavailable)
		return
	}
	h.auth.HandleCallback(w, r)
}

// Logout clears the session and redirects to the auth service logout.
// Falls back to a bare cookie clear + redirect to "/" when auth is not
// configured, so the endpoint remains safe to link to unconditionally.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		auth.ClearSession(w)
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	h.auth.HandleLogout(w, r)
}

// LogoutNotify is the front-channel logout target: the auth service loads
// it in a hidden iframe when a user signs out centrally, and we clear our
// local session cookie in response. Always returns 200 so the iframe load
// doesn't flag as an error, and works regardless of whether the
// AuthProvider is configured (the cookie to clear is always the same).
func (h *Handler) LogoutNotify(w http.ResponseWriter, _ *http.Request) {
	auth.ClearSession(w)
	w.WriteHeader(http.StatusOK)
}

// AuthMe returns the current signed-in user for the status-bar badge.
// 204 No Content means "no session" (not an error); 200 returns a small
// JSON view of the OIDC userinfo so the frontend can render an avatar.
func (h *Handler) AuthMe(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	u := h.auth.UserFromRequest(w, r)
	if u == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Sub     string `json:"sub"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}{u.Sub, u.Email, u.Name, u.Picture})
}
