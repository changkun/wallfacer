---
title: "GitHub OAuth App and Token Store"
status: stale
depends_on:
  - specs/identity/authentication.md
affects:
  - internal/github/auth.go
  - internal/github/token.go
  - internal/handler/github_auth.go
  - internal/handler/config.go
  - internal/store/models.go
  - frontend/src/components/settings/SettingsTabGithub.vue
  - frontend/src/views/SettingsPage.vue
  - frontend/src/stores/github.ts
effort: large
created: 2026-06-26
updated: 2026-06-26
author: changkun
dispatched_task_id: null
---

# GitHub OAuth App and Token Store

Lead child of [github-integration](../github-integration.md). Nothing else
dispatches until GitHub tokens exist.

## Design Problem

Wallfacer must hold a GitHub credential with API scope so every other GitHub
feature (repo list, PR/issue read, PR/comment write) can call the API as the
user. The credential comes from a real OAuth flow (Codex-style), not a host
`gh` login, so it works headless and in cloud. The open decisions: which GitHub
app type, how the OAuth flow is brokered for a localhost server, where and how
the token is stored and scoped to the principal, and how it is refreshed and
revoked.

## Context

- The only existing token machinery is the latere.ai OIDC device-code flow in
  `internal/handler/device_auth.go`, storing via `authkit.FileTokenStore` at
  `~/.config/latere/token.json`. The GitHub token is a **distinct** credential
  and must not be conflated with the latere.ai identity token.
- Principal context (user sub, org) comes from
  [authentication.md](../../identity/authentication.md); the GitHub token is
  scoped to it so a signed-in user's token is not reused across principals.
- `/api/config` (`internal/handler/config.go`, `buildConfigResponse`) is the
  existing place the UI reads capability/auth status; extend it.

## Resolved: app type is a GitHub App

The umbrella resolved the app-type fork to a **GitHub App** (per-org install,
user-to-server token + installation token, per-repo permissions
`contents` / `pull_requests` / `issues` / `metadata`). This is chosen for the
org-governance fit with repo-identity's org boundary. Consequences this child
must design for: a connect flow with an explicit **install + grant** step (not a
one-click OAuth consent), two token kinds (the user-to-server token for
acting-as-user reads/writes, and the installation token for server-side
actions), and an installation id persisted alongside the principal. The
`internal/github` client seam still isolates the token model so the
implementation can fall back to a plain OAuth App if install friction proves too
high in practice; the UI and scopes below assume the GitHub App path.

The remaining options (brokering, storage) stay open and are recorded below.

## Options

### App type: OAuth App vs GitHub App (resolved -> GitHub App)

Kept for rationale; the decision is recorded above.

- **OAuth App** (user-to-server token): simplest flow, token acts as the user,
  scopes are coarse (`repo`, `read:org`). Classic tokens historically did not
  expire; the modern flow issues an expiring user token with a refresh token.
- **GitHub App** (installation + user-to-server): finer-grained per-repo
  permissions, installation tokens for server-side actions, better for org
  install governance, but more moving parts (installation id, app JWT, two
  token kinds). Aligns better with repo-identity's org boundary.

Decided: **GitHub App** (see "Resolved" above), with the OAuth-App fallback kept
behind the `internal/github` client seam.

### OAuth brokering for a localhost server

- **Direct**: the wallfacer server registers its own callback
  (`http://127.0.0.1:<port>/api/github/auth/callback`); the client secret lives
  server-side. Works for self-hosted/local.
- **Brokered via latere.ai**: the OAuth App is registered once centrally, and
  the callback rides the existing latere.ai auth infra (like the public OIDC
  client). Avoids every install registering its own GitHub app; necessary for
  the cloud/multi-instance story.

### Token storage

- **File store** (reuse `authkit.FileTokenStore` pattern, separate file e.g.
  `github-token.json`): matches current local conventions, principal-keyed.
- **`store.Store`** (durable, principal/org columns): needed once cloud/multi-
  user holds tokens for many principals; aligns with the Postgres store the
  coordination plane already uses.

## Open Questions

1. ~~OAuth App or GitHub App for v1?~~ **Resolved: GitHub App** (see above).
2. Self-registered localhost callback, or brokered through latere.ai's auth
   infra so installs do not each register a GitHub app?
3. Which scopes/permissions are the minimum for read + PR-create + comment
   (`repo`, `read:org`, `read:user`; or GitHub App `contents`, `pull_requests`,
   `issues`, `metadata`)?
4. File store vs `store.Store` for the token, given cloud must hold many
   principals' tokens? Can local start with a file and migrate?
5. Refresh strategy: refresh on 401, or proactively before expiry? Where does
   the refresh token live and how is a failed refresh surfaced (disconnect +
   re-prompt)?
6. Does the GitHub token also satisfy repo-identity's "GitHub OAuth upgrade"
   verification tier, and if so what does it hand that subsystem?

## UI

Owns the **Settings tab** half of the surface (the umbrella's
[UI Architecture](../github-integration.md#ui-architecture)); the `/github` page
chrome belongs to components 2-3. A new `SettingsTabGithub.vue` is registered in
`SettingsPage.vue` alongside the existing Execution / Sandbox / Workspace tabs,
following the `AccountControl.vue` connect pattern. All status reads come from
`/api/config` (extended here) via `stores/github.ts`.

States this child owns from the shared matrix: **Disconnected**, **Connecting**,
**Connected**, **Token expired / 401**.

```
Settings > GitHub
+--------------------------------------------------------------+
|  GitHub                                                      |
|                                                             |
|  [ Disconnected ]                                            |
|    Connect a GitHub App installation to browse and open      |
|    pull requests and issues.                                 |
|    [ Connect GitHub ]                                        |
|                                                             |
|  [ Connecting ]                                              |
|    Opening GitHub to install the app... (spinner)           |
|    Waiting for the install + grant to complete.             |
|                                                             |
|  [ Connected ]                                               |
|    Signed in as @login                                       |
|    Installed on: latere   ·  3 repositories granted         |
|    Permissions: contents, pull_requests, issues, metadata    |
|    [ Manage installation ↗ ]      [ Disconnect ]            |
+--------------------------------------------------------------+
```

- **Connect** triggers `POST /api/github/auth/connect`, opens the GitHub install
  + authorize URL in the OS browser, and the server handles the callback. The
  tab polls `/api/config` (or subscribes to the existing config refresh) until
  `connected` flips, then renders the Connected state.
- **Connecting** disables re-trigger and shows progress through the install +
  grant round trip; a timeout returns to Disconnected with a retry.
- **Connected** shows `login`, the installation target (org), granted repo count,
  and permissions, with `[ Manage installation ↗ ]` (deep link to the GitHub
  installation settings) and `[ Disconnect ]` (`POST /api/github/auth/disconnect`,
  confirm via the existing `ConfirmDialog.vue`).
- **Token expired / 401** is handled transparently: a silent refresh runs on
  401; a failed refresh drops to Disconnected and the tab re-prompts (no error
  toast spam). This is the failure mode the `/github` page defers to (it links
  here rather than handling reconnect inline).

The `/github` page's **Disconnected** call-to-action (when a user lands on the
page with no token) is a thin link into this tab; the connect logic itself lives
only here so there is one connect path.

## Affects

Introduces the `internal/github` package's auth + token layer and the
`/api/github/auth/*` routes (`status`, `connect`/callback, `disconnect`),
extends `/api/config` with GitHub auth status, and adds the connect/disconnect
UI as a new `SettingsTabGithub.vue` (see UI above). The token-store decision
ripples into whether
`internal/store/models.go` gains a GitHub-token entity.
