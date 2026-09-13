// Authorization primitive: RequireSuperadmin is a thin wrapper that
// inspects the validated principal already in context (placed there
// by OptionalAuth / Auth / CookieAuth) and short-circuits the request
// with 403 when the caller is not a superadmin.
//
// Local mode deployments never install this wrapper, so anonymous
// callers continue to reach every handler. Cloud-mode wiring decides
// on a per-route basis where to apply it.

package auth

import (
	"net/http"

	"latere.ai/x/pkg/authkit"
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
		if !c.Has(authkit.RolePlatformAdmin) {
			writeForbidden(w, "superadmin required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeForbidden(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":"forbidden","message":` + quote(msg) + `}`))
}
