// ForceLogin middleware: redirect unauthenticated browser requests to
// /login when cloud mode is on. API routes stay as 401 (upstream
// layers handle that); the goal is to stop anonymous HTML navigation
// from hitting an empty-looking board and to preserve the requested
// path across the auth round-trip.
//
// Local mode never installs this middleware.

package handler

import (
	"net/http"
	"net/url"
	"strings"

	"changkun.de/x/wallfacer/internal/auth"
)

// unprotectedPaths are exact HTML / API endpoints that must never
// redirect in cloud mode. The list covers:
//   - The auth dance itself (/login, /callback, /logout, /logout/notify).
//   - /api/config, because the frontend fetches it before it can
//     possibly know the user's identity.
//   - /api/auth/me, because the status-bar renderer expects 204 for
//     the anonymous branch and must not be served HTML.
var unprotectedPaths = map[string]struct{}{
	"/login":         {},
	"/callback":      {},
	"/logout":        {},
	"/logout/notify": {},
	"/api/config":    {},
	"/api/auth/me":   {},
	"/favicon.ico":   {},
}

// unprotectedPrefixes are path prefixes that must never redirect.
// Asset routes fall in this bucket so browsers can load CSS / JS
// chunks even before the user is authenticated. (In cloud mode the
// initial HTML request does redirect; once the user is signed in,
// the assets for the real page load through freely.)
var unprotectedPrefixes = []string{
	"/css/",
	"/js/",
	"/assets/",
	"/static/",
}

// ForceLogin returns a middleware that redirects unauthenticated
// HTML navigation to /login?next=<original-path>. The redirect only
// fires when:
//
//   - `Handler.HasAuth()` is true (cloud mode), AND
//   - no *auth.Claims are in context, AND
//   - the request is a GET, AND
//   - the Accept header indicates HTML navigation (contains
//     "text/html"), AND
//   - the path is not in the unprotected allowlist above.
//
// API routes (/api/*) that aren't in the allowlist are left alone.
// Their 401 responses are the upstream layers' job; the browser's
// fetch layer typically handles that with a page reload that then
// hits the redirect.
//
// Local mode collapses this to identity via `HasAuth() == false`.
func (h *Handler) ForceLogin(next http.Handler) http.Handler {
	if !h.HasAuth() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !shouldForceLogin(r) {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := auth.PrincipalFromContext(r.Context()); ok {
			next.ServeHTTP(w, r)
			return
		}
		http.Redirect(w, r, loginRedirectURL(r), http.StatusFound)
	})
}

// shouldForceLogin encodes the request-shape gate: only GETs for
// HTML paths that are not in the allowlist get considered. API
// requests with Accept: application/json pass through untouched, so
// 401 stays 401 instead of becoming a browser redirect.
func shouldForceLogin(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if _, ok := unprotectedPaths[r.URL.Path]; ok {
		return false
	}
	for _, p := range unprotectedPrefixes {
		if strings.HasPrefix(r.URL.Path, p) {
			return false
		}
	}
	// Browser navigation requests carry Accept with text/html somewhere
	// in the list. XHR / fetch calls typically don't. This is a
	// heuristic but matches what the platform's other services use.
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "text/html")
}

// loginRedirectURL constructs the /login?next=<path> target. `next`
// is validated to be path-only (no scheme/host), defeating the
// open-redirect class of bug. An invalid `next` is dropped; the
// caller just lands on /login with no return target.
func loginRedirectURL(r *http.Request) string {
	next := r.URL.RequestURI()
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/login"
	}
	// A URL like /login?next=http://evil/ must not parse as an
	// off-site redirect. url.Parse treats it as a path-only URL, but
	// belt-and-suspenders: drop anything that parses with a host set.
	u, err := url.Parse(next)
	if err != nil || u.Host != "" || u.Scheme != "" {
		return "/login"
	}
	q := url.Values{"next": {next}}.Encode()
	return "/login?" + q
}
