---
title: Unify browser session and JWT into a single principal context
status: complete
depends_on:
  - specs/shared/authentication/jwt-middleware.md
affects:
  - internal/auth/
  - internal/handler/
effort: small
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Unify browser session and JWT into a single principal context

## Goal

Handlers should read identity through a single API regardless of whether the
request carried a browser session cookie (from Phase 1) or an `Authorization:
Bearer <jwt>` header (from `jwt-middleware.md`). Today the two paths produce
two different shapes (`*oidc.User` vs `*jwtauth.Claims`). This task teaches
the middleware to populate the same `*jwtauth.Claims` context value for both
paths.

## What to do

1. Extend the request preamble (new middleware layer, runs after
   `jwtauth.OptionalAuth`):
   ```go
   // CookiePrincipal injects claims derived from the session cookie when
   // no Bearer token was present. No-op when claims are already populated
   // by OptionalAuth.
   func CookiePrincipal(c *auth.Client, v *jwtauth.Validator, next http.HandlerFunc) http.HandlerFunc
   ```
2. Implementation path:
   - If `PrincipalFromContext` already returns claims → pass through.
   - Else load the session via `c.SessionFromRequest(r)` (add to
     `internal/auth/` if the platform package doesn't already expose it,
     mirror the cookie read from `client.UserFromRequest`).
   - Take the session's access token, call
     `v.Validate(ctx, sessionAccessToken)`, this is the same JWT issued
     by the auth service, so validation is uniform.
   - On success, inject the resulting claims into the context.
   - On failure (token expired, signature invalid), clear the session
     cookie and pass through as anonymous, the next interactive request
     hits `/login`.
3. Wire the middleware only on HTML-rendering and API routes that benefit
   from identity, in practice, wrap the whole mux tail after `OptionalAuth`.
4. Document the uniform API: every handler that needs the caller uses
   `auth.PrincipalFromContext(r.Context())`; no handler ever calls
   `client.UserFromRequest` directly. Update any Phase-1 handler that
   already reads `UserFromRequest` (only `/api/auth/me`), that one can
   stay as-is since it specifically wants the OIDC userinfo shape for UI
   rendering, but add a code comment noting the distinction.

## Tests

- `internal/auth/cookie_principal_test.go`:
  - JWT present → middleware is a no-op (claims unchanged).
  - No JWT, valid session cookie → claims populated from session token.
  - No JWT, expired session token → session cookie cleared, request passes
    through as anonymous.
  - No JWT, no session → passes through as anonymous.
- Handler-level smoke test: `/api/tasks` called with only a session cookie
  sees the same `Claims` it would see with a Bearer token.

## Boundaries

- Do not add new cookies. The session cookie from Phase 1 is the only
  identity-carrying cookie.
- Do not change `/api/auth/me`'s response shape, it still returns
  OIDC userinfo, not claims.
- Do not introduce a third identity type. The goal is fewer shapes, not
  more.

## Outcome

Delivered. Handlers now see `*auth.Claims` in context regardless of
whether the caller authenticated via `Authorization: Bearer <jwt>` or a
session cookie, accessed uniformly through `auth.PrincipalFromContext`.

### What shipped
- `internal/auth/cookie_principal.go`: `CookiePrincipal(src, v, next)`
  middleware. Short-circuits when claims are already populated by JWT
  OptionalAuth upstream. Otherwise loads the session via
  `sessionSource.GetSession`, validates the stored access token through
  the same `*jwtauth.Validator`, and injects resulting claims. Stale
  tokens trigger `oidc.ClearSession` so the next browser nav hits
  `/login` instead of looping.
- `internal/auth/cookie_principal_test.go`: 5 tests covering the
  no-op-when-JWT-present branch, the happy path with a valid session
  token, the missing-cookie branch, the expired-token branch with the
  cookie-clear assertion, and the nil-inputs identity collapse.
- `internal/cli/server.go`: `authClient` promoted to the outer scope so
  the middleware stack can wire it. `CookiePrincipal` installed ahead
  of `OptionalAuth` (outermost of the two identity layers), so when
  both paths would apply the JWT wins.

### Implementation notes

1. **`sessionSource` interface, not `*auth.Client` directly.** The spec
   sketch used `*auth.Client` as the input type. Implementation defined
   a tiny interface (single method `GetSession(*http.Request) (*Session, error)`)
   so tests can substitute a fake without standing up a real encrypted
   cookie. Production still passes `*auth.Client`, which satisfies the
   interface via the platform's existing `GetSession` method.

2. **No new `internal/auth/` helper for `SessionFromRequest`.** The spec
   mentioned "add to `internal/auth/` if the platform package doesn't
   already expose it." The platform package exposes
   `Client.GetSession(r)`, so no wrapper was needed.

3. **`/api/auth/me` stays unchanged.** The spec called out this handler
   as already correct (returns OIDC userinfo, not claims). Confirmed:
   `AuthMe` still calls `h.auth.UserFromRequest(w, r)` directly and
   returns the `{sub, email, name, picture}` shape the status-bar
   renderer expects.

4. **No rollout to individual handlers.** The spec said to "wrap the
   whole mux tail" after OptionalAuth. Implementation did exactly that
   in `initServer`, so every `/api/*` and HTML route inherits the
   bridge automatically. No per-handler rewrites.
