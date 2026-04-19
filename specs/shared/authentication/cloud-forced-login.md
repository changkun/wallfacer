---
title: Force login for unauthenticated browser requests in cloud mode
status: drafted
depends_on:
  - specs/shared/authentication/principal-context.md
affects:
  - internal/handler/
  - internal/cli/
effort: small
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Force login for unauthenticated browser requests in cloud mode

## Goal

When `WALLFACER_CLOUD=true`, a browser that hits the web UI without a valid
session is redirected to `/login` instead of seeing an anonymous task board.
API requests continue to receive 401 (from the JWT middleware) rather than
a redirect — they have no browser to redirect.

Local mode is untouched: anonymous browsers still see the board.

## What to do

1. Add `ForceLogin` middleware in `internal/handler/`:
   ```go
   // ForceLogin redirects unauthenticated HTML requests to /login when
   // cloud mode is on. No-op when cfg.Cloud == false. Always passes
   // through when PrincipalFromContext returns claims.
   func (h *Handler) ForceLogin(next http.HandlerFunc) http.HandlerFunc
   ```
2. Criteria to redirect:
   - `cfg.Cloud == true` AND
   - `auth.PrincipalFromContext(r.Context())` returns `(nil, false)` AND
   - Request looks like a browser navigation: `GET`, `Accept` header
     contains `text/html`, and path is NOT in the unprotected list below.
3. Unprotected paths (always pass through, never redirect):
   - `/login`, `/callback`, `/logout`, `/logout/notify`
   - `/api/config` (frontend bootstrap needs it before the user is known)
   - `/api/auth/me` (returns 204, which the status-bar renderer expects)
   - `/static/*`, `/favicon.ico`, any embedded asset route
4. API routes under `/api/*` are NOT redirected. JWT middleware already
   returns 401 for missing / invalid tokens on the routes that require
   auth. The browser's JS fetch layer treats 401 as "reload the page" and
   the subsequent HTML GET hits the redirect path.
5. Preserve the requested URL through login via a `next` query parameter:
   `/login?next=/mode/board`. On callback completion, `pkg/oidc` already
   supports a post-login redirect target; pass it through.

## Tests

- `internal/handler/force_login_test.go`:
  - cloud + anonymous + HTML GET `/` → 302 to `/login?next=%2F`.
  - cloud + anonymous + HTML GET `/mode/board` → 302 to
    `/login?next=%2Fmode%2Fboard`.
  - cloud + anonymous + GET `/api/config` → 200 (unprotected).
  - cloud + anonymous + GET `/api/tasks` → 401 (not redirected).
  - cloud + authenticated GET `/` → 200 (pass through).
  - local + anonymous GET `/` → 200 (pass through).
  - cloud + anonymous GET `/login` → 200 (pass through, not a loop).

## Boundaries

- Do not add a logout redirect. `/logout` already clears cookies and
  redirects to the auth service; the return path is out of scope here.
- Do not add any `next`-parameter validation beyond path normalization —
  do not allow absolute-URL redirects (open-redirect class of bug).
  Validate that `next` starts with `/` and contains no scheme.
- Do not forbid the API key path. A cloud-mode request carrying a valid
  `WALLFACER_SERVER_API_KEY` is treated as authenticated for the purpose
  of this redirect.
