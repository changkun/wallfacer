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
  - frontend/src/components/GithubPanel.vue
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

## Options

### App type: OAuth App vs GitHub App

- **OAuth App** (user-to-server token): simplest flow, token acts as the user,
  scopes are coarse (`repo`, `read:org`). Classic tokens historically did not
  expire; the modern flow issues an expiring user token with a refresh token.
- **GitHub App** (installation + user-to-server): finer-grained per-repo
  permissions, installation tokens for server-side actions, better for org
  install governance, but more moving parts (installation id, app JWT, two
  token kinds). Aligns better with repo-identity's org boundary.

Lean: **GitHub App** for the org-scoping fit, unless the install friction is
judged too high for v1, in which case OAuth App ships first and the model is
swapped behind the `internal/github` client seam.

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

1. OAuth App or GitHub App for v1? (org-scoping vs install friction.)
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

## Affects

Introduces the `internal/github` package's auth + token layer and the
`/api/github/auth/*` routes (`status`, `connect`/callback, `disconnect`),
extends `/api/config` with GitHub auth status, and adds the connect/disconnect
UI in the GitHub panel. The token-store decision ripples into whether
`internal/store/models.go` gains a GitHub-token entity.
