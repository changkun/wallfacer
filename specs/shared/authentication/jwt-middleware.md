---
title: JWT validation middleware on API routes
status: validated
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
