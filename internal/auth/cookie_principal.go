// Bridges the browser session cookie to the same *Claims context key
// used by JWT middleware. Handlers read identity through one helper
// (PrincipalFromContext) regardless of whether the caller authenticated
// via Authorization: Bearer <jwt> or a session cookie.

package auth

import (
	"net/http"

	"latere.ai/x/pkg/oidc"
)

// sessionSource is the subset of *oidc.Client the cookie middleware
// needs. Defined as an interface so tests can substitute a fake
// without standing up a full Client.
type sessionSource interface {
	GetSession(r *http.Request) (*oidc.Session, error)
}

// CookiePrincipal populates *Claims in the request context from the
// session cookie when no JWT-sourced claims are already present. The
// session cookie's access token is itself a JWT signed by the auth
// service, so it validates through the same Validator used for Bearer
// tokens — no parallel code path.
//
// Behavior matrix:
//
//	JWT already populated claims → pass through unchanged
//	No JWT, no session cookie    → pass through anonymous
//	Session cookie, validate ok  → inject *Claims, proceed
//	Session cookie, validate bad → ClearSession, proceed anonymous
//
// Either argument may be nil:
//   - client == nil or v == nil collapses the middleware to identity.
//     Makes wiring in local mode a no-op.
func CookiePrincipal(client sessionSource, v *Validator, next http.Handler) http.Handler {
	if client == nil || v == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, already := PrincipalFromContext(r.Context()); already {
			next.ServeHTTP(w, r)
			return
		}
		sess, err := client.GetSession(r)
		if err != nil || sess == nil || sess.AccessToken == "" {
			next.ServeHTTP(w, r)
			return
		}
		claims, err := v.Validate(sess.AccessToken)
		if err != nil {
			// Token in the cookie is stale or signature-invalid. Clear
			// the cookie so the next browser navigation hits /login
			// instead of re-trying a dead session on every request.
			oidc.ClearSession(w)
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithClaims(r.Context(), claims)))
	})
}
