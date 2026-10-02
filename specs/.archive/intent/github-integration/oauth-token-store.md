---
title: "GitHub OAuth App and Token Store"
status: archived
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
updated: 2026-10-02
author: changkun
dispatched_task_id: null
---

# GitHub OAuth App and Token Store

> **Archived 2026-10-02 as shipped.** The brokered token and its store are
> in the code; the in-app connect flow designed below is not, by decision.
> [Outcome](#outcome) at the end records what was built, what differs and
> what was left out. The design text is kept as written.

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

## Resolved: brokering is a central "Latere AI" GitHub App

Brokering is resolved to the **brokered-via-latere.ai** model, and deliberately
**product-general**: a single GitHub App named **"Latere AI"** is registered once
at the latere.ai org level and shared across latere.ai products, not a
wallfacer-specific app. Users install "Latere AI" on their
org once; the install/authorize callback rides latere.ai's auth infra (the same
place the public OIDC client lives), and wallfacer receives the brokered token
scoped to the principal. This mirrors how `AccountControl.vue` already federates
identity through shared latere-ui + latere.ai OIDC rather than per-app sign-in.

Consequences:

- **No per-install app registration.** Installs do not each create a GitHub App;
  they install the one central "Latere AI" app. The client secret / app private
  key lives in latere.ai infra, never in a wallfacer instance.
- **Brokering home is the Latere identity service, not the infrastructure
  layer.** That service already brokers external OAuth providers (google,
  github, x) but its GitHub provider is **social-login / identity only** (scopes
  `read:user`, `user:email`; the callback exchanges the code, fetches userinfo,
  and discards the token -- there is no connected-account / external token
  store). The "Latere AI" GitHub *App* is a separate credential class
  (repo-scoped `contents`/`pull_requests`/`issues`/`metadata`) that the identity
  service does **not** broker today. That work is specced identity-side:
  register the App, the install flow, and a `/internal/github/installation-token`
  mint endpoint gated by a `github:mint-token` service scope. The capability
  previously existed there and was removed incidental to an unrelated cut; that
  removed code is the design precedent. The infrastructure layer only carries
  the app secrets. Until the endpoint lands,
  wallfacer's `internal/github` client + token store run against a mock; the
  `Direct` localhost path below is retained only as a dev stopgap behind the same
  client seam, not as the shipping model.
- **Cross-repo decision -- credential kind (recommended: user-to-server).** The
  identity-side spec recommends a **user-to-server token** so actions are authored
  **as the user**, matching how Claude Code's GitHub connector behaves (GitHub
  App install, durable, repo-select, acts as you) and matching this `Token` model
  (`Login`, `RefreshToken`) and the "Signed in as @login" UI already built here
  -- so no wallfacer rework; the live `Broker` just calls the identity
  service's `/internal/github/token`. The cost is encrypted refresh-token
  storage on that side. The alternative (installation token, bot attribution, no token at
  rest) would instead leave `Login`/`RefreshToken` unused and reword the settings
  UI to "Installed on \<org\>". Durability and repo-selection are the same either
  way (both are GitHub App installs). Confirm before implementing the live
  `Broker`.
- **Cross-product token scope.** Because the app is shared, the token store keys
  on the principal (user/org), and the same brokered credential can serve other
  latere products; wallfacer must not assume it owns the registration.

The remaining option (storage) stays open and is recorded below.

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

### OAuth brokering for a localhost server (resolved -> brokered via latere.ai)

Kept for rationale; the decision (central "Latere AI" app, brokered) is recorded
above.

- **Direct**: the wallfacer server registers its own callback
  (`http://127.0.0.1:<port>/api/github/auth/callback`); the client secret lives
  server-side. Retained only as a dev stopgap behind the client seam.
- **Brokered via latere.ai** (decided): the central "Latere AI" GitHub App is
  registered once, and the callback rides the existing latere.ai auth infra (like
  the public OIDC client). Avoids every install registering its own app;
  necessary for the cloud/multi-instance and cross-product story.

### Token storage

- **File store** (reuse `authkit.FileTokenStore` pattern, separate file e.g.
  `github-token.json`): matches current local conventions, principal-keyed.
- **`store.Store`** (durable, principal/org columns): needed once cloud/multi-
  user holds tokens for many principals; aligns with the Postgres store the
  coordination plane already uses.

## Open Questions

1. ~~OAuth App or GitHub App for v1?~~ **Resolved: GitHub App** (see above).
2. ~~Self-registered localhost callback, or brokered through latere.ai?~~
   **Resolved: brokered** via a single central "Latere AI" GitHub App (see
   above). Remaining sub-question: the brokering home is the Latere identity
   service, which today brokers GitHub only for social login -- the GitHub App
   repo-access brokering is new identity-side work and must be built there
   before the live flow works; wallfacer runs against a mock until then.
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
[UI Architecture](../github-integration.md)); the `/github` page
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

## Outcome

**Summary.** Implemented directly (not dispatched) between 2026-06-29 and
2026-07-01. Wallfacer holds a GitHub App user-to-server token for the
signed-in principal, fetched from the Latere identity service and cached on
disk, and every GitHub call uses it. The brokering decision held. The connect
flow did not: wallfacer has no connect or disconnect control of its own, and
the connection is made once on the latere.ai account and borrowed. Drift:
moderate. The credential and its seams match the design; the Settings UI, the
refresh mechanism and the storage choice landed smaller than designed.

**What shipped.**
- `internal/github/token.go`, `internal/github/store.go` (`1cb2b67d`):
  `Token`, `Principal{OrgID, Sub}`, the `Store` interface, and `FileStore`,
  which keeps one `github-<sha256 of org and sub>.json` per principal, mode
  0600 in a 0700 directory, written by temp file and rename.
- `internal/github/provider.go` (`dc68515b`): the `Broker` interface and
  `Provider.Get`, which serves the stored token while it is valid and
  otherwise asks the broker and saves the result.
- `internal/github/broker.go` (`91f03949`): `HTTPBroker`, which calls
  `GET <auth URL>/me/integrations/github/token` with the signed-in user's
  identity token as the bearer, and maps 404, 401 and 503 to
  `ErrNotConnected`.
- `internal/handler/github_auth.go`, `internal/handler/config.go`,
  `internal/apicontract/routes.go` (`eae8c8e4`):
  `GET /api/github/auth/status`, `POST /api/github/auth/connect`,
  `POST /api/github/auth/disconnect`, and a `github` block on `/api/config`.
- `internal/cli/server.go` (`eae8c8e4`, `a80ff04c`): the file store is
  created under `<config dir>/github/`. The broker is attached when an auth
  URL is configured; its bearer comes from the shared identity token file
  that the device-code sign-in and the browser session bridge both write.
- `frontend/src/components/settings/SettingsTabGithub.vue` and
  `frontend/src/stores/github.ts` (`1a8ebf9e`, `2501105b`, reshaped by
  `ff4958e7`, restyled by `7ca61208`): the GitHub tab in Settings.

**Design evolution (diverged).**
- **No in-app connect.** `a80ff04c` built it: the connect endpoint returns
  the install URL on the identity service, and `474eb2ae` passed `return_to`
  so the install came back to wallfacer. `ff4958e7` removed the caller one
  day later. Its stated reason is the connectors-hub model: GitHub is
  connected once on the latere.ai account and shared across products, and
  wallfacer borrows that connection instead of making one. The tab now has
  three read-only states: signed out (a sign-in
  button), signed in and not connected (a link to the account page), and
  connected (the login and a manage link). The designed Connecting state,
  the Disconnect button with its confirm dialog, the Manage installation
  link, the installed-on line and the permissions line are not rendered.
  The `connect` and `disconnect` endpoints remain, and no frontend code
  calls them.
- **Status source.** The design has the tab read `/api/config`. The tab reads
  `GET /api/github/auth/status`, which asks the broker first and so reflects
  a connection made on the account page. `/api/config` carries the same block
  from the cache alone, without a broker call.
- **Refresh (open question 5).** No refresh token reaches wallfacer: the
  broker response carries an access token, an expiry and a login. Renewal is
  `Provider.Get` asking the broker again once the cached token is within 30
  seconds of expiry. A 401 from the GitHub API is not retried; it is returned
  as 401 by `mapGitHubAPIError`. A valid cached token is served without
  asking the broker, so a connection removed on the account page stays
  usable locally until that token expires.
- **Token metadata (open question 3).** `Token` declares `RefreshToken`,
  `InstallationID`, `Account` and `Permissions`. `HTTPBroker` sets none of
  them, so the `account` and `permissions` fields of the status response are
  always empty with the live broker. Wallfacer neither requests nor checks
  permissions; they are a property of the app registration.
- **Storage (open question 4).** File store only.
  `internal/store/models.go` gained no GitHub entity.
- **Local principal.** A request with no identity stores its token under a
  fixed `local` key (`githubPrincipal`).
- **Files.** There is no `internal/github/auth.go`. The auth layer is
  `broker.go`, `provider.go`, `token.go` and `store.go`.

**Not built.**
- The "Direct" localhost OAuth callback kept in the design as a development
  stopgap. Tests use a fake broker instead.
- A durable store for many principals. `internal/github/doc.go` still names
  it as later work. `HTTPBroker` is documented as the local single-user path:
  its bearer is the one identity token the instance holds. A hosted board
  serving several principals needs a bearer per request and a durable store,
  and no live spec owns that work.
- Open question 6. No code hands the GitHub token to the coordination plane;
  `internal/github` is imported only by `internal/cli/server.go` and
  `internal/handler`.

Later, 2026-10-02: the whole GitHub integration was retired from wallfacer, this part included. See [remove-github-integration](../../shared/platform-native/remove-github-integration.md).
