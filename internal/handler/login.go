package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"latere.ai/x/wallfacer/internal/auth"
)

// AuthProvider is the subset of *auth.Client the HTTP handlers need. Kept
// as an interface so tests can substitute a fake without spinning up a real
// OIDC client. An untyped-nil value means auth is not configured.
//
// The Handle* methods stay on the interface so the Login/Callback/Logout/
// LogoutNotify endpoints can apply wallfacer-specific behavior (503 when
// unconfigured, fall back to a bare cookie-clear on Logout) that the
// upstream pkg/oidc handlers do not provide.
type AuthProvider interface {
	HandleLogin(http.ResponseWriter, *http.Request)
	HandleCallback(http.ResponseWriter, *http.Request)
	HandleLogout(http.ResponseWriter, *http.Request)
	HandleLogoutNotify(http.ResponseWriter, *http.Request)
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
// doesn't flag as an error. When the AuthProvider is configured, delegates
// to its HandleLogoutNotify so any product-specific cleanup runs; otherwise
// falls back to a bare cookie clear (the cookie name doesn't depend on
// provider state).
func (h *Handler) LogoutNotify(w http.ResponseWriter, r *http.Request) {
	if h.auth != nil {
		h.auth.HandleLogoutNotify(w, r)
		return
	}
	auth.ClearSession(w)
	w.WriteHeader(http.StatusOK)
}

// AuthMe returns the current signed-in principal for the account menu. 204 No
// Content means "no session" (not an error). 200 returns the latere-ui
// Principal shape (principal_id, identity, active org, org list, superadmin,
// auth_url) so the shared AccountMenu can render the avatar, the org switcher,
// and the dashboard link without extra round-trips.
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

	// Best-effort org list for the switcher. A failure (token just expired,
	// auth unreachable) degrades to identity-only rather than failing the call.
	orgs := []orgEntry{}
	orgName := ""
	if sr, ok := h.auth.(sessionReader); ok {
		if sess, err := sr.GetSession(r); err == nil && sess != nil && sess.AccessToken != "" {
			if list, err := fetchOrgs(r.Context(), h.authURL, sess.AccessToken); err == nil {
				orgs = list
				for _, o := range list {
					if o.ID == u.OrgID {
						orgName = o.Name
						break
					}
				}
			}
		}
	}

	display := u.DisplayName
	if display == "" {
		display = u.Name
	}
	avatar := u.AvatarURL
	if avatar == "" {
		avatar = u.Picture
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(struct {
		PrincipalID  string     `json:"principal_id"`
		Email        string     `json:"email"`
		Name         string     `json:"name"`
		DisplayName  string     `json:"display_name,omitempty"`
		AvatarURL    string     `json:"avatar_url,omitempty"`
		Initials     string     `json:"initials,omitempty"`
		OrgID        string     `json:"org_id"`
		OrgName      string     `json:"org_name,omitempty"`
		Orgs         []orgEntry `json:"orgs"`
		IsSuperadmin bool       `json:"is_superadmin"`
		AuthURL      string     `json:"auth_url,omitempty"`
	}{
		PrincipalID:  u.Sub,
		Email:        u.Email,
		Name:         u.Name,
		DisplayName:  display,
		AvatarURL:    avatar,
		Initials:     userInitials(display, u.Email),
		OrgID:        u.OrgID,
		OrgName:      orgName,
		Orgs:         orgs,
		IsSuperadmin: u.IsSuperadmin,
		AuthURL:      h.authURL,
	})
}

// userInitials returns a single uppercase initial from the name, falling back
// to the email, then "?". The shared AccountMenu shows it when no avatar_url.
func userInitials(name, email string) string {
	s := strings.TrimSpace(name)
	if s == "" {
		s = strings.TrimSpace(email)
	}
	if s == "" {
		return "?"
	}
	return strings.ToUpper(s[:1])
}
