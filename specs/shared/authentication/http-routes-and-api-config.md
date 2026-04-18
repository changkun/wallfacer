---
title: Cloud-gated auth routes, /api/auth/me, and /api/config extension
status: validated
depends_on:
  - specs/shared/authentication/envconfig-and-auth-package.md
affects:
  - internal/handler/
  - internal/apicontract/routes.go
  - internal/cli/
  - main.go
effort: medium
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Cloud-gated auth routes, /api/auth/me, and /api/config extension

## Goal

Mount `/login`, `/callback`, `/logout`, `/logout/notify`, and `/api/auth/me`
only when `WALLFACER_CLOUD=true`. Fail-fast at startup if cloud mode is on
but OIDC config is missing. Extend `/api/config` with `cloud` and `auth_url`
fields so the frontend can decide whether to render the sign-in badge.

## What to do

1. **Handler constructor** — Extend `handler.New` (see
   `internal/handler/doc.go` and the existing constructor) to accept an
   `*auth.Client` (nullable). Store it on the `Handler` struct. Follow the
   pattern in `~/dev/latere.ai/latere-ai/internal/handler/handler.go`.
2. **Auth handlers** — Add `internal/handler/auth.go`:
   - `Login(w, r)`, `Callback(w, r)`, `Logout(w, r)` — delegate to
     `h.auth.HandleLogin/Callback/Logout`. Return `503 Service Unavailable`
     with body `"auth not configured"` when `h.auth == nil`.
   - `LogoutNotify(w, r)` — always calls `auth.ClearSession(w)` and returns
     `200 OK`. Safe even when `h.auth == nil` (front-channel logout endpoint
     is nil-independent in latere-ai).
   - `AuthMe(w, r)` — when `h.auth == nil` → `204 No Content`. Otherwise call
     `h.auth.UserFromRequest(w, r)`; if nil → `204`; else JSON-encode
     `{sub, email, name, picture}` with `200`.
3. **Route registration** — In the CLI server wiring (find where routes are
   registered today; search for `api/config` handler mount), add the four
   browser routes + `/api/auth/me` **only when `cfg.Cloud == true`**. Use
   the same router the rest of the handlers use (stdlib `net/http` with
   Go 1.22+ pattern syntax).
4. **apicontract** — Add the five routes to `internal/apicontract/routes.go`
   as cloud-mode routes. Follow the existing route-definition style. Run
   `make api-contract` to regenerate `ui/js/generated/routes.js`.
5. **Startup validation** — In the CLI entry point that constructs the
   server, after loading env config:
   ```go
   var authClient *auth.Client
   if cfg.Cloud {
       authClient = auth.New(auth.LoadConfig())
       if authClient == nil {
           slog.Error("WALLFACER_CLOUD=true requires AUTH_CLIENT_ID, AUTH_CLIENT_SECRET, and AUTH_REDIRECT_URL")
           os.Exit(1)
       }
   }
   ```
   When `cfg.Cloud == false`, do not call `auth.New` at all — keeps local
   mode dependency-inert.
6. **Extend `/api/config`** — In `handler.GetConfig`, add to the response:
   ```go
   Cloud   bool   `json:"cloud"`
   AuthURL string `json:"auth_url,omitempty"` // populated only when cloud && h.auth != nil
   ```
   `AuthURL` comes from `h.auth.AuthURL()`.

## Tests

- `internal/handler/auth_test.go`:
  - `TestAuthMe_NilClient_Returns204` — handler with `auth: nil` → 204.
  - `TestAuthMe_NoSession_Returns204` — fake client returning nil User → 204.
  - `TestAuthMe_WithSession_Returns200` — fake client returning a User → 200
    with JSON body containing `sub`, `email`, `name`, `picture`.
  - `TestLogin_NilClient_Returns503` — handler with `auth: nil` → 503.
  - `TestLogoutNotify_ClearsCookie` — verifies the session cookie is cleared
    (inspect `Set-Cookie` header) regardless of client presence.
- `internal/handler/config_test.go`:
  - `TestGetConfig_CloudFlagFalse` — default config → `cloud: false`, no
    `auth_url` field.
  - `TestGetConfig_CloudFlagTrue` — `cfg.Cloud=true` with fake client that
    returns `"https://auth.latere.ai"` from `AuthURL()` → response contains
    `cloud: true` and `auth_url: "https://auth.latere.ai"`.
- Server-wiring test (integration-lite, in `internal/cli/` or `main_test.go`
  if one exists): `TestCloudRoutes_NotMountedInLocalMode` — starts the
  server with `cfg.Cloud=false`, asserts `GET /login` returns 404 (not 503).
  Mirror pattern of existing `config_host_mode_test.go`.

## Boundaries

- Do not add any other authenticated routes. `/api/auth/me` is the only
  new authenticated endpoint.
- Do not wire JWT middleware — no route in this task requires a valid
  session; `/api/auth/me` degrades to 204.
- Do not touch the frontend (`ui/`) in this task.
- Do not modify `WALLFACER_SERVER_API_KEY` handling — it remains orthogonal.
