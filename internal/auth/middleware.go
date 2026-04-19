// JWT validation middleware for /api/* routes in cloud mode. Wraps an
// http.Handler so a valid Authorization: Bearer <jwt> surfaces as
// *Claims in the request context. Local mode (cfg.Cloud == false)
// never instantiates a Validator and never wraps routes, so the only
// gate remains WALLFACER_SERVER_API_KEY.

package auth

import (
	"context"
	"net/http"
	"strings"

	"latere.ai/x/pkg/jwtauth"
)

// claimsCtxKey scopes the context value used to carry validated claims
// through the middleware chain. Defined here (not in the platform
// package) so both Auth and OptionalAuth share one key that
// PrincipalFromContext can read uniformly. The platform package's own
// context helper is never used inside wallfacer, so handlers never need
// to check two places.
type claimsCtxKey struct{}

// BuildValidator constructs a Validator from the auth configuration.
// JWKS endpoint is auto-derived from cfg.AuthURL when not passed
// explicitly. Issuer validation is optional and only applied when an
// explicit issuer is passed or AUTH_ISSUER is set — fosite-issued JWT
// access tokens don't always carry iss that matches the discovery
// document. Audience validation is deliberately omitted: we're
// validating tokens received in our own callback from an auth service
// we trust by JWKS signature; any token that signs under the auth
// service's key was issued by it. Returns nil when cfg.AuthURL is
// empty.
//
// jwksURL and issuer override the derived defaults; pass "" for either
// to keep the default (empty issuer = skip iss check). The CLI boot
// path reads AUTH_JWKS_URL and AUTH_ISSUER from the environment and
// forwards them here.
func BuildValidator(cfg Config, jwksURL, issuer string) *Validator {
	if cfg.AuthURL == "" {
		return nil
	}
	if jwksURL == "" {
		jwksURL = strings.TrimRight(cfg.AuthURL, "/") + "/.well-known/jwks.json"
	}
	jc := jwtauth.Config{
		JWKSURL: jwksURL,
		Issuer:  issuer, // empty = skip iss check; operator sets AUTH_ISSUER to opt in
	}
	return jwtauth.New(jc)
}

// OptionalAuth validates a Bearer JWT when present and injects claims
// into the request context. Missing, malformed, or expired tokens pass
// through as anonymous rather than returning 401. Use this on routes
// that may identify the caller but never require it.
//
// Semantics:
//   - No Authorization header           -> pass through, no claims
//   - Authorization header not "Bearer" -> pass through, no claims
//   - Bearer present, validation fails  -> pass through, no claims
//   - Bearer present, validation ok     -> inject *Claims into ctx
//
// A nil validator (local mode) returns next unchanged.
func OptionalAuth(v *Validator, next http.Handler) http.Handler {
	if v == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, ok := bearerToken(r)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		claims, err := v.Validate(tok)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, withClaims(r, claims))
	})
}

// Auth validates a Bearer JWT and rejects the request on failure with
// 401. Use this on routes that strictly require an authenticated
// principal. A nil validator returns next unchanged (local mode).
//
// Phase 2 does not apply Auth to any route; OptionalAuth is the only
// gate installed. Strict-auth opt-in happens in later specs
// (scope-and-superadmin.md, cloud-forced-login.md).
func Auth(v *Validator, next http.Handler) http.Handler {
	if v == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, ok := bearerToken(r)
		if !ok {
			writeUnauthorized(w, "missing bearer token")
			return
		}
		claims, err := v.Validate(tok)
		if err != nil {
			writeUnauthorized(w, err.Error())
			return
		}
		next.ServeHTTP(w, withClaims(r, claims))
	})
}

// PrincipalFromContext returns the validated claims if the request was
// authenticated by OptionalAuth or Auth. The second return is false for
// anonymous requests (no header, expired token, local mode).
func PrincipalFromContext(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(claimsCtxKey{}).(*Claims)
	if !ok || c == nil {
		return nil, false
	}
	return c, true
}

// WithClaims returns a context carrying the given claims. Exported so
// handler-layer tests can exercise downstream middleware (e.g. the
// BearerAuth claims-bypass) without standing up a full JWKS server and
// signing a real token. Production code should only inject claims via
// OptionalAuth / Auth after validation.
func WithClaims(ctx context.Context, c *Claims) context.Context {
	return context.WithValue(ctx, claimsCtxKey{}, c)
}

// withClaims attaches claims to the request's context.
func withClaims(r *http.Request, c *Claims) *http.Request {
	return r.WithContext(WithClaims(r.Context(), c))
}

// bearerToken extracts the token from the Authorization header. Returns
// (token, true) only when the header matches the "Bearer " scheme with
// a non-empty token; other schemes (Basic, etc.) and empty bearers
// return ("", false).
func bearerToken(r *http.Request) (string, bool) {
	raw := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(raw, prefix) {
		return "", false
	}
	tok := strings.TrimSpace(raw[len(prefix):])
	if tok == "" {
		return "", false
	}
	return tok, true
}

func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized","message":` + quote(msg) + `}`))
}

// quote is a minimal JSON string-escape helper kept local to avoid
// pulling encoding/json just for one error body.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
