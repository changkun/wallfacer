---
title: wallfacer — Auth Unification Migration (cloud + local-mode device-code)
status: drafted
depends_on:
  - "auth/specs/auth-unification.md"
  - "auth/specs/auth-unification/authkit-hybrid-identity.md"
  - "auth/specs/auth-unification/authkit-device-code-and-token-store.md"
  - "auth/specs/auth-unification/authkit-cookie-and-env-compat.md"
  - "auth/specs/auth-unification/integration-doc-rewrite.md"
  - "latere-ui/specs/auth-client-v1.8.md"
affects:
  - internal/handler/login.go
  - internal/handler/orgs.go
  - internal/cli/server.go
  - internal/cli/web.go
  - internal/handler/config.go
  - frontend/stores/auth.ts
  - ui/js/status-bar.js
  - main.go (Wails bindings for device-code local-mode)
  - frontend/package.json
  - CLAUDE.md
  - AGENTS.md
  - README.md
  - docs/auth.md (auth flow diagrams)
effort: large
trigger: parent auth-unification spec; wallfacer ships the canonical org-switch pattern (cloud) and is the prime device-code consumer (local-mode Wails desktop)
created: 2026-05-31
updated: 2026-05-31
author: changkun
dispatched_task_id: null
---

# wallfacer — Auth Unification Migration (cloud + local-mode device-code)

## Overview

`wallfacer` is the largest single-product migration. It runs in two modes:

1. **Cloud mode** (`WALLFACER_CLOUD=true`): standard web-hybrid product. Migrates by deleting `AuthProvider` interface + `sessionReader` + `tokenRefresher` interface wrappers; mounting `pkg/oidc` handlers directly; adopting the canonical inline switch-org pattern at `/api/auth/switch-org`. The canonical org-switch UX (currently in `internal/handler/orgs.go:155-201`) becomes the documented template in `auth/INTEGRATION.md` — wallfacer's implementation stays as the reference; it just stops being a one-off.

2. **Local mode** (`WALLFACER_CLOUD` unset, Wails desktop binary): greenfield. Today no auth wiring exists; the embedded HTTP server has no `/login`. The migration adds RFC 8628 device-code login via `pkg/authkit.DeviceCodeClient` + `pkg/authkit.FileTokenStore`, opened in the system browser through the Wails runtime. After login, the local SPA's session cookie (set by an embedded `pkg/oidc.SetSession` call once tokens arrive) authenticates subsequent local HTTP requests just like cloud mode.

This spec may be broken down via `wf-spec-breakdown` into two children — `auth-unification-migration-cloud.md` + `auth-unification-migration-local.md` — if size grows further.

## Current State

`internal/handler/login.go`:
- L13-19 — `AuthProvider` interface: thin wrapper around `*oidc.Client`.
- L44 — `h.auth.HandleLogin(w, r)`.
- L48-54 — `h.auth.HandleCallback(w, r)`.
- L59-65 — `Logout` with fallback branch.
- L73-76 — `LogoutNotify` thin wrapper.
- L86 — `UserFromRequest` call for status-bar avatar identity.

`internal/handler/orgs.go`:
- L119-132 — `sessionReader` / `tokenRefresher` interfaces (thin wrappers).
- L155-201 — `AuthSwitchOrg` handler — canonical pattern that gets promoted to `auth/INTEGRATION.md` as the inline switch-org template.

`internal/cli/server.go:241-275` — wires `oidcClient` and `Handler` conditionally on `cloudMode`.

`internal/handler/config.go:162` — `Principal{Sub, OrgID}` group scoping; stays.

`internal/cli/web.go:53-96` — wallfacer-local web server (no auth today).

`frontend/stores/auth.ts:6` — bespoke `fetchMe()` Pinia store; does not use `latere-ui`'s `createSessionStore`.

`ui/js/status-bar.js:404-526` — vanilla JS org-switcher; reference for the `latere-ui` headless `OrgSwitcher` design.

`main.go` — Wails app entrypoint. Adds device-code bindings in this migration.

Existing `specs/identity/authentication/` siblings provide rich context: `cloud-forced-login.md`, `data-model-principal-org.md`, `org-switching.md`, `principal-context.md`, `scope-and-superadmin.md` — none are superseded; this spec composes them with the new pkg/authkit primitives.

## Components — Cloud mode

### Delete `AuthProvider` interface and wrapper methods

`internal/handler/login.go:13-19` — delete the interface. The `Handler` struct now takes `*oidc.Client` directly.

Delete the thin wrapper methods `Login`, `Callback`, `Logout`, `LogoutNotify` at L44, L48-54, L59-65, L73-76. Replace with direct `mux.HandleFunc("GET /login", oidcClient.HandleLogin)` etc. in `internal/cli/server.go:241-275`.

`UserFromRequest` call at L86 — replace with `oidcClient.SessionFromRequest(w, r)` (or `oidcClient.BuildMe` if richer profile is needed).

### Delete `sessionReader` / `tokenRefresher` interfaces

`internal/handler/orgs.go:119-132` — delete. These are thin wrappers around `*oidc.Client`. Pass `*oidc.Client` directly.

### Promote `AuthSwitchOrg` body to the canonical template

`internal/handler/orgs.go:155-201` — body stays; it IS the canonical pattern (membership check via `oidcClient.FetchOrgs`, 303 to `/login?org_id=…&return_to=…`). Mount it as the inline handler:

```go
mux.HandleFunc("POST /api/auth/switch-org", switchOrgHandler(oidcClient))
```

Where `switchOrgHandler` is the canonical inline implementation per `auth/INTEGRATION.md`. The behavior is identical to today's `AuthSwitchOrg`; the function name and location change.

### Canonical mount block

```go
if cloudMode {
    mux.HandleFunc("GET /login", oidcClient.HandleLogin)
    mux.HandleFunc("GET /callback", oidcClient.HandleCallback)
    mux.HandleFunc("GET /logout", oidcClient.HandleLogout)
    mux.HandleFunc("GET /logout/notify", oidcClient.HandleLogoutNotify)
    mux.HandleFunc("GET /api/me", inlineMeHandler(oidcClient))
    mux.HandleFunc("POST /api/auth/switch-org", switchOrgHandler(oidcClient))
}
```

### Identity chain

```go
authn := authkit.Chain(
    authkit.NewJWT(jwksValidator, tokenInfoClient),
    authkit.NewSessionAuthenticator(oidcClient),
)
mux.Handle("/api/", authkit.Middleware(apiRouter, authn))
```

### Env prefix migration

`internal/cli/server.go` — replace any `WALLFACER_AUTH_*` reader with `authkit.LoadConfigWithPrefix("WALLFACER")`. Deployment configs keep `WALLFACER_AUTH_*` until cleanup.

### Cookie name compat

`WALLFACER_SESSION` legacy cookie (verify exact name in `pkg/oidc.LoadConfig` override or hardcode) → unified `__Host-latere-session`. Inline two-step fallback during cutover per parent's cookie/env compat spec.

### Frontend

`frontend/stores/auth.ts:6` — replace bespoke `fetchMe()` with `latere-ui v1.8.0`'s `createSessionStore` + `useSession`. Adopt `AccountMenu` and the new headless `OrgSwitcher`.

`ui/js/status-bar.js:404-526` — port to `mountOrgSwitcher(el, opts)` from `latere-ui v1.8.0`. The vanilla harness stays vanilla; only the org-switcher widget swaps to the headless primitive.

`frontend/package.json` — bump `latere-ui` from `v1.2.3` to `v1.8.0` (large skew; this is its own commit; verify `SiteFooter` and other consumed components don't regress).

CSRF cookie rename to `__Host-latere-csrf` after backend dual-read.

## Components — Local mode (greenfield)

### Embedded HTTP server + device-code

`internal/cli/web.go:53-96` — the local-mode embedded HTTP server gains a `/api/auth/device/start` + `/api/auth/device/poll` pair of endpoints? Or simpler: a single Wails-bound function that drives `pkg/authkit.DeviceCodeClient` end-to-end and writes the session cookie back to the embedded SPA via `oidcClient.SetSession`.

Recommended design: **Wails binding**, not HTTP routes. The local SPA calls `window.go.main.App.AuthDeviceLogin()`; the Go side runs `DeviceCodeClient.Login(ctx)` in a goroutine, opens the verification URL in the system browser via Wails runtime, polls until tokens arrive, then:

1. Saves the refresh token via `FileTokenStore` at `<UserConfigDir>/latere/token.json`.
2. Calls `oidcClient.SetSession(...)` against the SPA's response writer (need to thread it through; or just write the cookie file/storage that the local SPA's fetch uses).
3. Emits a `wails:auth:complete` event so the SPA reloads `/api/me`.

Wails main.go:

```go
app := &App{
    deviceClient: authkit.NewDeviceCodeClient(oidcClient, store),
}
wails.Run(&options.App{
    Bind: []interface{}{app},
    ...
})

// In app.go:
func (a *App) AuthDeviceLogin(ctx context.Context) error {
    return a.deviceClient.Login(ctx)
}
```

### Local-mode `/api/me` handler

Reads the stored token via `FileTokenStore.Load`, calls `oidcClient.FetchUserInfo(ctx, tok.AccessToken)`, returns the user profile JSON. Same shape as cloud-mode `/api/me`.

### Local-mode org switching

CLI-style: re-authenticate via device-code with `org_id` extra param (the auth service's `/authorize?org_id=…` or `/token?org_id=…` refresh-grant work for this). Local SPA's `OrgSwitcher` calls `window.go.main.App.SwitchOrg(orgID)`; Go side calls auth's `POST /token` with `grant_type=refresh_token&org_id=<id>` using the stored refresh token. Document the flow in `docs/auth.md`.

### Frontend (local-mode)

The SPA uses `latere-ui`'s vanilla `me()` and `switchOrg()` against the local HTTP server (which proxies through to auth via the stored token). Use the headless `OrgSwitcher`.

`frontend/stores/auth.ts` — same migration as cloud-mode; cloud + local share the store via the standard `createSessionStore`.

### `Principal{Sub, OrgID}` group scoping (unchanged)

`internal/handler/config.go:162` — keeps its scoping. After migration, the principal id and org id come from `authkit.Identity` via the standard `authkit.IdentityFromContext(ctx)` rather than from bespoke session reads.

## Sequencing

1. Bump `latere.ai/x/pkg` dependency.
2. Cloud-mode: delete `AuthProvider`, `sessionReader`, `tokenRefresher` interfaces.
3. Cloud-mode: inline mount canonical endpoints; inline switch-org handler.
4. Cloud-mode: compose hybrid identity chain.
5. Cloud-mode: replace env reader; inline cookie-name fallback.
6. Frontend bump from v1.2.3 to v1.8.0 (own commit; verify SiteFooter etc.).
7. Frontend: adopt `createSessionStore` + `AccountMenu`; port `ui/js/status-bar.js` to `mountOrgSwitcher`.
8. Local-mode: verify `auth/oauth/device_authorization` exists (per `auth/specs/auth-unification/device-authorization-endpoint.md`); block on it if missing.
9. Local-mode: add Wails `AuthDeviceLogin` binding; wire `pkg/authkit.DeviceCodeClient` + `FileTokenStore`.
10. Local-mode: implement `/api/me` proxy handler; implement org-switch via refresh-grant.
11. Manual smoke test on the desktop binary (checklist below).
12. Update docs.
13. After backend dual-read: frontend CSRF cookie rename.

## Testing Strategy

- **Cloud-mode existing**: rerun all existing wallfacer tests; org-switch behavior must match today's (canonical pattern is unchanged in behavior).
- **Cloud-mode new**: integration test for `/logout/notify` (verifies cookie clear).
- **Local-mode new**: Wails E2E is hard to automate; document a manual smoke checklist in `docs/auth.md`:
  1. Launch `wallfacer` (local mode).
  2. Click "Sign in". Verification URL opens in system browser.
  3. Enter the user code; approve.
  4. App receives session, `/api/me` returns identity.
  5. Click org switcher → select non-personal org → app reloads with new org's tasks.
  6. Click "Sign out". Local session cleared, file at `~/.config/latere/token.json` removed.
- **Frontend**: Vitest + Playwright smoke against the embedded SPA after the v1.8.0 bump.

## Rollback Plan

- Cloud-mode: each step is independently revertible.
- Local-mode device-code: behind a single `WALLFACER_DEVICE_CODE_ENABLED` env flag (or build tag) during initial rollout; flip off to revert to no-auth local mode.
- Frontend v1.8.0 bump: revert if SiteFooter or other components regress; bump back to v1.2.3 temporarily and address upstream.

## Risks

- **Device-authorization endpoint may not exist.** Local-mode is blocked on `auth/specs/auth-unification/device-authorization-endpoint.md` if so. Verify first.
- **Wails runtime opening URLs**: cross-platform (macOS / Linux / Windows). `pkg/authkit.DeviceCodeClient`'s default opener uses `runtime.GOOS`-based dispatch; test on all three.
- **Token file shared with `latere-cli`**: both write to `~/.config/latere/token.json`. Document this in `docs/auth.md` and in `latere-cli`'s docs. Intended: sign in once, both apps see it. Risk: if a user runs both with different `AUTH_URL` configs, the file gets overwritten with inconsistent tokens. Mitigation: the device-code flow records the issuer in the token file and refuses to load mismatched ones.
- **Wallfacer SiteFooter regression**: `latere-ui` v1.2.3 → v1.8.0 is a big jump. Snapshot tests on SiteFooter before bumping; review release notes for v1.3..v1.7 changes.
- **`Principal{Sub, OrgID}` scoping**: after migration, the principal identity comes from `authkit.IdentityFromContext`. Any downstream code that hard-coded `oidc.User.Sub` access needs grep + audit.
- **Cloud + local mode share Wails binary**: ensure conditional logic (`cloudMode` branch) does not regress cloud users when local-mode code lands. Build matrix test in CI.
