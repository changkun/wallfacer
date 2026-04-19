// Authorization primitives: RequireSuperadmin and RequireScope. Both
// are thin wrappers that inspect the validated principal already in
// context (placed there by OptionalAuth / Auth / CookiePrincipal) and
// short-circuit the request with 403 when the caller lacks the
// required privilege.
//
// Local mode deployments never install these wrappers, so anonymous
// callers continue to reach every handler. Cloud-mode wiring decides
// on a per-route basis which ones to apply.

package auth

import (
	"net/http"
	"slices"
)

// RequireSuperadmin returns 403 when the caller is not a superadmin,
// 401 when there are no claims in context. The 401 branch is defensive:
// upstream middleware typically produces 401 first, but keeping this
// check makes the wrapper safe to apply in isolation during tests or
// in a misordered middleware stack.
func RequireSuperadmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := PrincipalFromContext(r.Context())
		if !ok {
			writeUnauthorized(w, "authentication required")
			return
		}
		if !c.IsSuperadmin {
			writeForbidden(w, "superadmin required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireScope returns an http.Handler-wrapper factory that enforces
// a given scope name on the claim set's `scp` array. 403 when the
// claim set does not include the scope; 401 when no claims are present.
//
// Downstream handlers opt in as scopes are assigned. This task
// scaffolds the wrapper only; no route applies it yet.
func RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, ok := PrincipalFromContext(r.Context())
			if !ok {
				writeUnauthorized(w, "authentication required")
				return
			}
			if !slices.Contains(c.Scopes, scope) {
				writeForbidden(w, "scope "+scope+" required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeForbidden(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":"forbidden","message":` + quote(msg) + `}`))
}
