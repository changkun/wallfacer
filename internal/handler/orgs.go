// Org listing + switching endpoints. Both are cloud-only; local mode
// mounts no handler (same pattern as the other Phase 1 auth routes).
//
// Listing (GET /api/auth/orgs) proxies to the auth service's /me/orgs
// using the session's access token, so the user only sees orgs they
// are a member of.
//
// Switching (POST /api/auth/switch-org) clears the wallfacer session
// cookie and returns a 303 redirect to /login?org_id=<target>. The
// browser follows; oidc.HandleLogin forwards org_id to /authorize;
// the auth service persists the choice on the SSO session and issues
// a new token scoped to the chosen org; wallfacer's /callback lands
// a fresh session cookie.

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"changkun.de/x/wallfacer/internal/auth"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
)

// httpGet is a package-level indirection so tests can substitute a
// fixed response without a full HTTP server stand-up. Defaults to the
// standard client with a generous timeout; the auth service is a
// same-cluster call so even 10s is liberal.
var httpGet = func(req *http.Request) (*http.Response, error) {
	return http.DefaultClient.Do(req)
}

// orgEntry is the subset of the auth service's /me/orgs payload the
// UI actually renders. Other fields (owner_id, joined_at) flow through
// unused today.
type orgEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug,omitempty"`
}

// orgsResponse is what GET /api/auth/orgs returns to the frontend.
// CurrentID is the caller's current org claim, so the renderer can
// mark it as active without a second round trip.
type orgsResponse struct {
	Orgs      []orgEntry `json:"orgs"`
	CurrentID string     `json:"current_id"`
}

// AuthOrgs returns the signed-in user's org list, or 204 when single-
// org or unauthenticated.
func (h *Handler) AuthOrgs(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// Pull the session to get the access token we forward to the auth
	// service. Without a session we have no Bearer to present to
	// /me/orgs, so 204 (matches the unauthenticated branch of
	// /api/auth/me).
	client, ok := h.auth.(sessionReader)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	sess, err := client.GetSession(r)
	if err != nil || sess == nil || sess.AccessToken == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	orgs, err := fetchOrgs(r.Context(), h.authURL, sess.AccessToken)
	if err != nil {
		httpjson.Write(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	// No org memberships at all → 204. This is the one case where
	// there is genuinely nothing to render: the user isn't in any
	// org, the cloud tenant boundary doesn't apply to them, and the
	// status-bar renderer should bail. For 1+ orgs we return 200 so
	// the frontend can always surface the active org — even single-
	// org users get a visible label, which doubles as operator-
	// visible confirmation that /api/auth/orgs is wired correctly.
	if len(orgs) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	currentID, _ := claimsOrgFromContext(r.Context())
	httpjson.Write(w, http.StatusOK, orgsResponse{Orgs: orgs, CurrentID: currentID})
}

// sessionReader is the subset of AuthProvider the org-listing handler
// needs. *auth.Client satisfies it via oidc.Client.GetSession; tests
// substitute a fake. Kept narrow so the interface doesn't accidentally
// grow.
type sessionReader interface {
	GetSession(*http.Request) (*auth.Session, error)
}

// switchOrgRequest is the POST /api/auth/switch-org body. Only org_id
// is required; the target is looked up and verified server-side.
type switchOrgRequest struct {
	OrgID string `json:"org_id"`
}

// switchOrgResponse is returned with 200 when the switch request is
// accepted. The frontend follows RedirectURL; we don't 302 directly
// because the frontend POSTs via fetch, which would make the browser
// follow silently instead of navigating.
type switchOrgResponse struct {
	RedirectURL string `json:"redirect_url"`
}

// AuthSwitchOrg validates the caller is a member of the requested org,
// clears the wallfacer session cookie, and returns a JSON body with
// the /login URL the frontend should navigate to. The actual token
// refresh happens as part of that redirect (auth service honors
// org_id on /authorize and mints a new token).
func (h *Handler) AuthSwitchOrg(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		httpjson.Write(w, http.StatusServiceUnavailable, map[string]string{"error": "auth not configured"})
		return
	}

	var req switchOrgRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpjson.Write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	req.OrgID = strings.TrimSpace(req.OrgID)
	if req.OrgID == "" {
		httpjson.Write(w, http.StatusBadRequest, map[string]string{"error": "org_id required"})
		return
	}

	// Validate membership client-side so we give a clean 403 rather
	// than letting the auth service silently ignore the param. The
	// auth service will re-check; this avoids the confusing case
	// where the browser completes the flow but lands on the old org.
	client, ok := h.auth.(sessionReader)
	if !ok {
		httpjson.Write(w, http.StatusServiceUnavailable, map[string]string{"error": "auth not configured"})
		return
	}
	sess, err := client.GetSession(r)
	if err != nil || sess == nil || sess.AccessToken == "" {
		httpjson.Write(w, http.StatusUnauthorized, map[string]string{"error": "not signed in"})
		return
	}
	orgs, err := fetchOrgs(r.Context(), h.authURL, sess.AccessToken)
	if err != nil {
		httpjson.Write(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	isMember := false
	for _, o := range orgs {
		if o.ID == req.OrgID {
			isMember = true
			break
		}
	}
	if !isMember {
		httpjson.Write(w, http.StatusForbidden, map[string]string{"error": "not a member of target org"})
		return
	}

	// Clear our local cookie so the forthcoming /login → /callback
	// lands a clean, org-scoped session. Without this, the frontend
	// would keep the old JWT until the /callback write, which is a
	// small race but worth closing.
	auth.ClearSession(w)

	redirect := "/login?org_id=" + req.OrgID
	httpjson.Write(w, http.StatusOK, switchOrgResponse{RedirectURL: redirect})
}

// fetchOrgs calls auth.latere.ai/me/orgs with the given access token
// and returns the parsed org list. Non-2xx responses become errors so
// the caller can surface 502 to the frontend.
func fetchOrgs(ctx context.Context, authURL, accessToken string) ([]orgEntry, error) {
	if authURL == "" {
		return nil, errors.New("auth url not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(authURL, "/")+"/me/orgs", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := httpGet(req)
	if err != nil {
		return nil, fmt.Errorf("call /me/orgs: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("/me/orgs returned %d: %s", resp.StatusCode, string(body))
	}
	var out []orgEntry
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode /me/orgs: %w", err)
	}
	return out, nil
}

// claimsOrgFromContext reads the current org claim from a validated
// principal in ctx. Returns "" when no principal or no org context.
func claimsOrgFromContext(ctx context.Context) (string, bool) {
	c, ok := auth.PrincipalFromContext(ctx)
	if !ok || c == nil {
		return "", false
	}
	return c.OrgID, true
}
