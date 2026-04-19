// Bridges the browser session cookie to the same *Claims context key
// used by JWT middleware. Handlers read identity through one helper
// (PrincipalFromContext) regardless of whether the caller authenticated
// via Authorization: Bearer <jwt> or a session cookie.

package auth

import (
	"log/slog"
	"net/http"
	"sync/atomic"

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
			// Token in the cookie is stale, signature-invalid, or the
			// validator config is wrong. Do NOT auto-clear the cookie:
			// a persistent validation error would produce an endless
			// /login ↔ /callback loop once ForceLogin enters the
			// picture, with no visible error. Pass through as
			// anonymous — ForceLogin will redirect HTML GETs to
			// /login, which is survivable (the user sees the login
			// page) while the operator fixes the config.
			logCookieValidateOnce(err)
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithClaims(r.Context(), claims)))
	})
}

// cookieValidateLogged is set once to avoid flooding logs with the
// same validation error on every request while the operator is
// fixing config. The underlying issue is a misconfiguration, not a
// per-request condition, so one log line is enough.
var cookieValidateLogged atomic.Bool

// logCookieValidateOnce logs the validation error at warn level the
// first time it fires in a process. Reset on binary restart.
func logCookieValidateOnce(err error) {
	if cookieValidateLogged.Swap(true) {
		return
	}
	slog.Warn("auth: session-cookie token validation failed; "+
		"signed-in users will be treated as anonymous. "+
		"Check AUTH_JWKS_URL, AUTH_ISSUER, and the auth service's token signing.",
		"error", err)
}
