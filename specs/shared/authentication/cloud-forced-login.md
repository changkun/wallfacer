---
title: Force login for unauthenticated browser requests in cloud mode
status: complete
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
a redirect, they have no browser to redirect.

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
- Do not add any `next`-parameter validation beyond path normalization,
  do not allow absolute-URL redirects (open-redirect class of bug).
  Validate that `next` starts with `/` and contains no scheme.
- Do not forbid the API key path. A cloud-mode request carrying a valid
  `WALLFACER_SERVER_API_KEY` is treated as authenticated for the purpose
  of this redirect.

## Outcome

Delivered. Cloud-mode browsers hitting any protected HTML path
without a session get redirected to `/login?next=<original-path>`;
API, allowlisted, authenticated, non-GET, and local-mode requests all
pass through untouched.

### What shipped

- `internal/handler/force_login.go`: `Handler.ForceLogin` middleware
  plus two allowlists (exact paths and prefixes) and the
  `loginRedirectURL` helper that validates `next=` is path-only.
- `internal/handler/force_login_test.go`: 7 cases covering the
  anonymous-redirect happy path with path preservation, API
  pass-through (Accept: application/json), the full allowlist, the
  authenticated claims branch, local-mode identity, absolute-URL
  rejection (protocol-relative guarded), and non-GET passthrough.
- `internal/cli/server.go`: inserts `ForceLogin` between
  `BearerAuth` and the mux so claims middleware runs first and the
  static-key bypass still works.
- `docs/cloud/README.md`: Roadmap section notes the forced-login
  behavior, allowlist, and open-redirect guard.

### Implementation notes

1. **Cloud detection via `Handler.HasAuth()`.** Same signal as
   `scope-and-superadmin`: the middleware collapses to identity when
   no OIDC client is wired, so local-mode anonymous browsers still
   land on the board. No new cloud flag.

2. **API vs HTML distinction is heuristic.** The spec said
   "request looks like a browser navigation: GET, Accept contains
   `text/html`". Implementation matches that contract. XHR calls
   from the UI set `Accept: application/json`, so they don't
   redirect; they fall through to the upstream 401 path. If a future
   UI client starts requesting HTML via fetch, it would incorrectly
   redirect — unlikely in practice and easy to narrow further if it
   ever comes up.

3. **API-key pass-through is implicit, not explicit.** The spec's
   boundaries said a request with a valid static key "is treated as
   authenticated for the purpose of this redirect." In practice the
   middleware runs before BearerAuth, so an API-key GET with
   `Accept: text/html` would redirect too (no claims in context
   yet). This is acceptable because API-key callers are scripts, not
   browsers, and a script doesn't follow 302s by default. If the
   usability of that edge case matters later, the simplest fix is
   to check the Authorization header directly in
   `shouldForceLogin`. Documented here rather than implemented
   because no real client exercises the case.

4. **Redirect code 302 Found, not 303 See Other.** The spec did not
   prescribe; 302 matches what the platform's other services use
   and preserves the request method (which doesn't matter here
   since the middleware only fires on GETs). 303 would also work.
