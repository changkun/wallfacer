---
title: JWT validation middleware on API routes
status: archived
depends_on:
  - specs/shared/authentication/http-routes-and-api-config.md
affects:
  - internal/auth/
  - internal/handler/
  - internal/cli/
  - go.mod
effort: medium
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---


# JWT validation middleware on API routes

## Goal

Install `latere.ai/x/pkg/jwtauth` as a request-validating middleware in front
of `/api/*` when `WALLFACER_CLOUD=true`. On a valid `Authorization: Bearer`
token the handler gets `*jwtauth.Claims` in its context; on an invalid or
missing token an authenticated route returns 401. Unauthenticated routes
(`/login`, `/callback`, `/logout`, `/logout/notify`, `/api/config`,
`/api/auth/me`) pass through without requiring a token.

Local mode is untouched, `WALLFACER_SERVER_API_KEY` remains the only gate.

## What to do

1. Add `latere.ai/x/pkg/jwtauth` to `go.mod` (vanity path resolves alongside
   `pkg/oidc` already in use by Phase 1).
2. Extend `internal/auth/` with middleware constructors mirroring the
   reference consumer at `~/dev/latere.ai/latere-ai/internal/auth/middleware.go`:
   ```go
   // BuildValidator constructs a *jwtauth.Validator from env vars
   // (AUTH_URL, AUTH_JWKS_URL, AUTH_ISSUER, AUTH_CLIENT_ID as audience).
   // Returns nil if cfg.Cloud == false.
   func BuildValidator(cfg Config) *jwtauth.Validator

   // Auth wraps a handler that requires a valid principal. 401 on invalid
   // or missing token. Extracts *jwtauth.Claims into the context.
   func Auth(v *jwtauth.Validator, next http.HandlerFunc) http.HandlerFunc

   // OptionalAuth validates if Authorization header is present, otherwise
   // passes through. Handlers can inspect context.
   func OptionalAuth(v *jwtauth.Validator, next http.HandlerFunc) http.HandlerFunc
   ```
3. Add `internal/auth/context.go`:
   ```go
   // PrincipalFromContext returns the validated claims if the request was
   // authenticated. Returns (nil, false) for anonymous requests.
   func PrincipalFromContext(ctx context.Context) (*jwtauth.Claims, bool)
   ```
4. In the CLI server wiring, when `cfg.Cloud == true`, construct the validator
   once and wrap every existing `/api/*` route (except the whitelist above)
   with `OptionalAuth`, *not* `Auth` in this task. Handlers remain open; the
   goal of this spec is only to surface claims when present. The forced-401
   decision for protected routes happens in `cloud-forced-login.md` and in
   whichever handler needs strict enforcement.
5. Keep the existing `WALLFACER_SERVER_API_KEY` bearer check. Order:
   JWT middleware first (populates context); API-key check second (matches
   existing behavior). A request with a valid JWT is always allowed; a
   request with the static key continues to work.

## Tests

- `internal/auth/middleware_test.go`:
  - valid JWT (fake JWKS) → handler sees `Claims` via `PrincipalFromContext`.
  - expired JWT → 401 on `Auth`, passes through on `OptionalAuth` with no claims.
  - malformed `Authorization` header → 401 on `Auth`.
  - no `Authorization` header → `OptionalAuth` passes through with no claims.
- Integration-lite in `internal/cli/`:
  - `TestAPIRoutes_ClaimsContext_InCloudMode`, server started with
    `cfg.Cloud=true`, a fake validator, and a request carrying a signed
    token; verify `GET /api/tasks` sees the claims.
  - `TestAPIRoutes_NoValidator_InLocalMode`, server with `cfg.Cloud=false`
    and an `Authorization: Bearer junk` header gets exactly today's
    behavior (no validation attempted, handler executes as before).

## Boundaries

- Do not force authentication on any route. `OptionalAuth` only. The
  decision matrix lives in the later child specs (cloud-forced-login,
  scope-and-superadmin).
- Do not touch the session cookie code path. This spec is about the API
  token path only; `principal-context.md` unifies the two.
- Do not add `org_id` filtering anywhere. That's `data-model-principal-org.md`.

## Outcome

Delivered. `latere.ai/x/pkg/jwtauth` is integrated through a tiny
wallfacer-side wrapper (`internal/auth/middleware.go`) that surfaces
validated claims to handlers via one uniform helper
(`auth.PrincipalFromContext`). In cloud mode the CLI wires
`OptionalAuth` into the middleware stack and a valid Bearer JWT
authenticates the caller without blocking static-key or cookie paths.

### What shipped
- `internal/auth/middleware.go` — `BuildValidator` (auto-derives JWKS
  URL / issuer from `AUTH_URL`; returns nil when unset), `OptionalAuth`
  wrapper that passes through anonymously on any validation failure,
  `Auth` wrapper that 401s on failure, `PrincipalFromContext` and
  exported `WithClaims` helper.
- `internal/auth/auth.go` — `Claims = jwtauth.Claims` and
  `Validator = jwtauth.Validator` type aliases so the rest of the
  codebase imports a single auth surface.
- `internal/auth/middleware_test.go` — 13 tests covering happy path,
  expired tokens, malformed headers, empty header, nil validator for
  both `OptionalAuth` and `Auth`, plus `PrincipalFromContext`.
- `internal/cli/server.go` — constructs `jwtValidator` only when
  `cfg.Cloud` is true, using `AUTH_URL` / `AUTH_JWKS_URL` /
  `AUTH_ISSUER` / `AUTH_CLIENT_ID` from env. Inserts `OptionalAuth`
  ahead of `BearerAuthMiddleware` in the middleware stack.
- `internal/cli/server_authstack_test.go` — three integration tests
  covering the spec's cloud/local scenarios and the JWT+static-key
  composition.
- `internal/handler/middleware.go` — `BearerAuthMiddleware` bypasses
  the static-key check when `PrincipalFromContext` returns claims,
  keeping JWT-authenticated requests workable in deployments that also
  set `WALLFACER_SERVER_API_KEY`.
- `docs/cloud/README.md` and `AGENTS.md` (= `CLAUDE.md`) document the
  two new optional env vars and note Phase 2 JWT shipping scope.

### Implementation notes

1. **`AUTH_JWKS_URL` and `AUTH_ISSUER` are read inline in
   `internal/cli/server.go`, not added to `envconfig.Config`.** The spec
   implied they'd be first-class fields on `Config`; in practice the
   two values only matter for building the validator and are not
   edited from the Settings UI or written back to the `.env` file. The
   existing pattern for `AUTH_URL`, `AUTH_CLIENT_ID`, etc. is the same
   inline `envconfig.Lookup` call, so the two new vars matched that
   pattern rather than growing the typed Config struct. If they ever
   become user-editable (e.g. custom auth service in Settings), they'd
   promote to typed fields then.

2. **Exported `auth.WithClaims` helper.** The spec described
   `PrincipalFromContext` as the only public surface. Implementation
   also exported `WithClaims(ctx, *Claims) context.Context` because the
   `BearerAuthMiddleware` bypass test needed to inject synthetic
   claims without signing a real JWT; making the injection point
   public kept the test simple and avoided a `_test.go`-only visibility
   hack. Production code still only injects claims through
   `OptionalAuth` / `Auth` after validation.

3. **Context key lives in `internal/auth/`, not in `jwtauth`.** The
   platform package's context key is unexported, so a caller of
   `v.Validate` can't inject claims directly into the jwtauth context
   shape. Wallfacer defines its own `claimsCtxKey` and
   `PrincipalFromContext` reads that key. Both `OptionalAuth` and
   `Auth` route through the same key, so handlers never need to
   check two sources. Trade-off: we duplicate the tiny context-key
   mechanism, in exchange for uniform access regardless of how the
   claims arrived.

4. **`Auth` does not reuse `jwtauth.Validator.Middleware`.** The
   platform's middleware writes claims under its own (unexported)
   context key; re-using it would force wallfacer handlers to read
   from two places. `Auth` instead calls `Validate` directly and
   injects through the same wallfacer context key as `OptionalAuth`.

5. **BearerAuth bypass is new behavior, not part of the original
   spec.** Without it, a cloud deployment that also sets
   `WALLFACER_SERVER_API_KEY` (reasonable for mixed browser + script
   setups) would reject every cookie-only browser request. The bypass
   is a four-line addition that checks `PrincipalFromContext` before
   the static-key comparison. Documented inline and covered by
   `TestBearerAuthMiddleware_ClaimsBypass` and
   `TestAPIRoutes_JWTWithStaticKeySet_BypassesKeyCheck`.
