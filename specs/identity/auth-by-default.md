---
title: Auth by Default and Platform Console Convergence
status: drafted
depends_on:
  - specs/identity/authentication.md
affects:
  - internal/cli/server.go
  - internal/cli/auth.go
  - internal/cli/web.go
  - internal/handler/login.go
  - internal/handler/force_login.go
  - internal/handler/config.go
  - internal/auth/
  - frontend/src/stores/auth.ts
  - frontend/src/components/AccountControl.vue
  - frontend/src/components/Sidebar.vue
  - frontend/package.json
effort: medium
created: 2026-06-14
updated: 2026-06-14
author: changkun
dispatched_task_id: null
---

# Auth by Default and Platform Console Convergence

The near-term thrust of the project is consolidation onto the shared Latere
platform: `wallfacer run` now signs in through the browser with no setup, the
principal path uses the shared `authkit.Identity`, and the web UI is adopting
the latere-ui console shell (account menu, org switcher, sidebar). This work is
the live edge in `main` but had no spec; this one captures the initiative so the
roadmap reflects where the energy actually is.

This is a follow-on to [authentication.md](authentication.md) (Phases 1 and 2,
complete) and the successor to the archived auth-unification-migration. It is
NOT cloud/hosted multi-user execution (that stays demand-gated under
multi-user-collaboration and the cloud track); it is the local-first product
defaulting into the Latere account experience.

## Goal

A plain `wallfacer run` offers browser sign-in against `auth.latere.ai` with
zero env setup, while staying usable fully anonymous. The web UI presents the
shared latere-ui console chrome (account menu, org switcher, theme, sign out) so
wallfacer looks and behaves like the rest of the Latere surface.

Precision: auth-by-default means sign-in is *available and ambient*, not
*mandatory*. Local anonymous still works; forced login stays cloud-gated
(`internal/handler/force_login.go`).

## Current State (shipped)

- **Zero-config public client** (`internal/cli/server.go` `resolveAuthConfig`,
  L420): a plain `wallfacer run` fills `AUTH_*` with secret-less "wallfacer"
  public-client defaults (`AuthURL=https://auth.latere.ai`, loopback
  `/callback`, `openid email profile offline_access` scopes). A local cookie key
  is generated and persisted under the config dir; explicit env or a
  confidential client still takes precedence. Loopback HTTP callbacks drop the
  `__Host-` cookie prefix (`InsecureCookies`).
- **Sign-in by default in `run`**: the `/login` flow is wired even without cloud
  mode, redirecting to the authorize endpoint (commits `db2bf8bf`, `34afc0f6`).
  The status bar / sidebar shows a sign-in chip when auth is enabled
  (`246214a8`, `21aee3b9`).
- **Shared identity**: the principal path uses `authkit.Identity`
  (`internal/auth/`), replacing the bespoke cookie principal.
- **Headless device-code** (`internal/cli/auth.go`): RFC 8628 device flow
  against `auth.latere.ai` for non-browser sign-in, plus the HTTP routes
  `/api/auth/device/{start,poll,cancel}`.
- **Console shell adoption**: `frontend/` pins latere-ui v1.9.11 and adopts the
  shared `AccountMenu` (org switcher, theme, sign out) and `ConsoleSidebar` via
  `AccountControl.vue` / `Sidebar.vue`; `stores/auth.ts` fetches the session
  reactively so the chip renders (`2ac545b0`, `1fe19ea8`, the `console:` series).

## Remaining / Open

- **Coherence pass**: the console adoption landed as a long series of polish
  commits. Capture the intended end-state (which latere-ui components are
  canonical, what wallfacer overrides) so further UI work has a target instead of
  reacting commit-by-commit.
- **Sign-in UX in local mode**: define the first-run experience (prompt vs
  silent-available), and what signed-in adds locally today (account linkage,
  org context) versus later (cloud metadata coordination).
- **Org context in local mode**: `/api/auth/orgs` + org switching exist; specify
  what an org selection actually scopes locally (board grouping? nothing yet?).
- **Config surface**: document the `AUTH_*` precedence and the public-vs-
  confidential-client story in user docs, since `resolveAuthConfig` now makes
  defaults implicit.

## Non-Goals

- Mandatory authentication (anonymous local use stays first-class).
- Cloud/hosted multi-user execution and org-scoped shared boards (see
  multi-user-collaboration and the cloud track).
- Third-party / self-hosted IdPs (see third-party-oidc).

## Acceptance Criteria

- `wallfacer run` with no env offers working browser sign-in against
  auth.latere.ai and remains fully usable signed-out.
- The web UI renders the shared latere-ui console chrome (account menu, org
  switcher, theme, sign out) consistent with other Latere surfaces.
- User docs explain the `AUTH_*` defaults, the public client, and how to point at
  a different auth service or use a confidential client.
- The intended console end-state is documented so UI work targets it.
