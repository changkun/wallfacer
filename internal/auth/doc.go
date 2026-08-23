// Package auth is wallfacer's HTTP authentication layer: the middleware that
// resolves a request's principal and the wrappers that gate a route on it.
//
// The identity types themselves are the platform's, not this package's.
// Configuration and the Relying Party client come from
// [latere.ai/x/pkg/oidc], the resolved principal is an
// [latere.ai/x/pkg/authkit.Identity], and JWT validation is
// [latere.ai/x/pkg/jwtauth]. This package used to re-export all of them under
// local names, which hid where a type came from and made the platform
// packages look optional; callers now name them directly.
//
// A request acquires a principal in one of two ways: a Bearer JWT validated
// against the auth service's JWKS ([OptionalAuth], [Auth]), or the encrypted
// session cookie ([CookieAuth]). Both deposit an *authkit.Identity that
// [PrincipalFromContext] reads back. Local-mode deployments construct neither
// a validator nor a client, so every wrapper degrades to a pass-through and
// anonymous callers continue to reach every handler.
package auth
