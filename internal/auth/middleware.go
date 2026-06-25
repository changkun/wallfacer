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

	"latere.ai/x/pkg/authkit"
	"latere.ai/x/pkg/jwtauth"
	"latere.ai/x/pkg/oidc"
)

// identityCtxKey scopes the context value used to carry the resolved principal
// through the middleware chain. A pointer (nil = anonymous) preserves the
// (Identity, ok) presence semantics PrincipalFromContext exposes — distinct
// from authkit.IdentityFromContext, which cannot tell "absent" from "zero".
type identityCtxKey struct{}

// BuildValidator constructs a Validator from the auth configuration.
// JWKS endpoint is auto-derived from cfg.AuthURL when not passed
// explicitly. Issuer validation is optional and only applied when an
// explicit issuer is passed or AUTH_ISSUER is set — fosite-issued JWT
// access tokens don't always carry iss that matches the discovery
// document. Audience validation uses cfg.ClientID when configured so
// tokens minted for other relying parties are rejected. Returns nil
// when cfg.AuthURL is empty.
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
	if cfg.ClientID != "" {
		jc.Audiences = []string{cfg.ClientID}
	}
	return jwtauth.New(jc)
}

// OptionalAuth validates a Bearer JWT when present and injects the resolved
// Identity into the request context. Missing, malformed, or expired tokens pass
// through as anonymous rather than returning 401. Use this on routes that may
// identify the caller but never require it.
//
// Semantics:
//   - No Authorization header           -> pass through, no identity
//   - Authorization header not "Bearer" -> pass through, no identity
//   - Bearer present, validation fails  -> pass through, no identity
//   - Bearer present, validation ok     -> inject *Identity into ctx
//
// A nil validator (local mode) returns next unchanged.
func OptionalAuth(v *Validator, next http.Handler) http.Handler {
	if v == nil {
		return next
	}
	jwt := authkit.NewJWT(v, nil)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id, err := jwt.Authenticate(r); err == nil {
			next.ServeHTTP(w, withIdentity(r, &id))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Auth validates a Bearer JWT and rejects the request on failure with
// 401. Use this on routes that strictly require an authenticated
// principal. A nil validator returns next unchanged (local mode).
func Auth(v *Validator, next http.Handler) http.Handler {
	if v == nil {
		return next
	}
	jwt := authkit.NewJWT(v, nil)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := bearerToken(r); !ok {
			writeUnauthorized(w, "missing bearer token")
			return
		}
		id, err := jwt.Authenticate(r)
		if err != nil {
			writeUnauthorized(w, err.Error())
			return
		}
		next.ServeHTTP(w, withIdentity(r, &id))
	})
}

// CookieAuth resolves the principal from the encrypted session cookie when no
// Bearer-sourced identity is already present, via the shared
// authkit.SessionAuthenticator (which trusts the AES-GCM-authenticated cookie —
// the platform-canonical posture, replacing wallfacer's former bespoke
// re-validation). A nil client (local mode) returns next unchanged.
//
// Behavior matrix:
//
//	identity already in ctx     -> pass through unchanged
//	no cookie / decode failure  -> pass through anonymous
//	cookie decodes              -> inject *Identity, proceed
func CookieAuth(client *oidc.Client, next http.Handler) http.Handler {
	if client == nil {
		return next
	}
	sess := authkit.NewSessionAuthenticator(client)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, already := PrincipalFromContext(r.Context()); already {
			next.ServeHTTP(w, r)
			return
		}
		if id, err := sess.Authenticate(r); err == nil {
			next.ServeHTTP(w, withIdentity(r, &id))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// PrincipalFromContext returns the resolved Identity if the request was
// authenticated by OptionalAuth, Auth, or CookieAuth. The second return is
// false for anonymous requests (no header, expired token, local mode).
func PrincipalFromContext(ctx context.Context) (*Identity, bool) {
	c, ok := ctx.Value(identityCtxKey{}).(*Identity)
	if !ok || c == nil {
		return nil, false
	}
	return c, true
}

// WithIdentity returns a context carrying the given Identity. Exported so
// handler-layer tests can exercise downstream middleware without standing up a
// full JWKS server and signing a real token. Production code should only inject
// an Identity via OptionalAuth / Auth / CookieAuth after validation.
func WithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, identityCtxKey{}, id)
}

// withIdentity attaches an Identity to the request's context.
func withIdentity(r *http.Request, id *Identity) *http.Request {
	return r.WithContext(WithIdentity(r.Context(), id))
}

// BearerToken extracts a token from an Authorization header value.
// Returns (token, true) only when the header matches the "Bearer "
// scheme with a non-empty token; other schemes (Basic, etc.) and
// empty bearers return ("", false). Exported so consumers outside
// this package (e.g. handler.SandboxProxy) can reuse the canonical
// extractor instead of re-implementing it.
func BearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	tok := strings.TrimSpace(header[len(prefix):])
	if tok == "" {
		return "", false
	}
	return tok, true
}

// bearerToken is the request-scoped wrapper used by the middleware
// helpers in this file.
func bearerToken(r *http.Request) (string, bool) {
	return BearerToken(r.Header.Get("Authorization"))
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
