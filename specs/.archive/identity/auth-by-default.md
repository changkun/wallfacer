---
title: Auth by Default and Platform Console Convergence
status: archived
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
updated: 2026-06-25
author: changkun
dispatched_task_id: null
---

# Auth by Default and Platform Console Convergence

> **Archived 2026-06-25 (shipped).** This was an adoption spec capturing
> already-landing convergence work; every Current State item shipped and all
> three Acceptance Criteria are met. Kept as system of record. See the Outcome
> section at the end.

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

This is an **adoption** spec: the shared building blocks are specified upstream
in `latere-ui` and `auth`, and those specs are the source of truth. wallfacer
does not redefine the console shell or the auth client; it pins them and wires
them in. Read alongside:

- `../latere-ui/specs/console-shell-v1.9.md` (complete): the shared console
  chrome (`AccountMenu`, `AccountPrefs`, `OrgSwitcher`, `ConsoleSidebar`,
  `brandTheme` includes `'wallfacer'`). Canonical for the UI surface.
- `../latere-ui/specs/auth-client-v1.8.md` (complete): the shared session client
  (`createSessionStore`, `me`/`orgs`/`switchOrg`, front-channel logout).
  Canonical for the client-side session contract.

This spec enumerates only wallfacer's delta on top of those.

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
- **Console shell adoption**: `frontend/` pins `latere-ui v1.9.12` and adopts the
  shared `AccountMenu` (org switcher, theme, sign out) and `ConsoleSidebar` via
  thin wrappers `AccountControl.vue` / `Sidebar.vue`; `stores/auth.ts` is the
  shared `createSessionStore` factory in `expiredSessionMode: 'graceful'` so an
  absent/expired session never bounces an anonymous local user to `/login`
  (`2ac545b0`, `1fe19ea8`, the `console:` series).

## Target End-State (decisions)

The shipped pieces above land the mechanism; these decisions pin the intended
shape so further UI work has a target.

- **Canonical components**: the latere-ui console shell is canonical. wallfacer
  owns only thin wrappers (`AccountControl.vue`, `Sidebar.vue`) that feed
  wallfacer's session/prefs stores into the shared `AccountMenu` / `AccountPrefs`
  / `ConsoleSidebar`; it uses `brandTheme: 'wallfacer'`. No bespoke account or
  sidebar chrome. Visual deltas are CSS overrides on the shared components, not
  forks. Bumping the `latere-ui` pin is the adoption path; do not vendor.
- **First-run UX: silent-available**. A plain `wallfacer run` does not prompt or
  redirect to sign-in. The sidebar shows a sign-in chip; sign-in is opt-in. This
  follows from `expiredSessionMode: 'graceful'` and keeps anonymous local use
  first-class. Forced login stays cloud-gated (`force_login.go`).
- **What signing in adds locally (today)**: account linkage and the populated
  account menu (identity, theme, sign out). Org selection in local mode is
  display-only and scopes nothing yet; what an org actually scopes (shared
  boards, RBAC) is owned by multi-user-collaboration, not here.
- **Config surface (docs)**: done. `docs/guide/configuration.md` "Account
  Sign-In" documents the public "wallfacer" client default, the `AUTH_*` table
  and precedence (explicit env / confidential client wins), and how to point at a
  different auth service. `docs/cloud/README.md` was corrected to stop claiming
  the sign-in surface is hidden in local mode.

## Non-Goals

- Mandatory authentication (anonymous local use stays first-class).
- Cloud/hosted multi-user execution and org-scoped shared boards (see
  multi-user-collaboration and the cloud track).
- Third-party / self-hosted IdPs (see third-party-oidc).

## Acceptance Criteria

- `wallfacer run` with no env offers working browser sign-in against
  auth.latere.ai, does not prompt or redirect on first run, and remains fully
  usable signed-out.
- The web UI renders the shared latere-ui console chrome (account menu, org
  switcher, theme, sign out) via thin wrappers over latere-ui, with no bespoke
  account/sidebar chrome.
- User docs explain the `AUTH_*` defaults, the public client, the precedence
  rules, and how to point at a different auth service or use a confidential
  client.

## Outcome

Shipped and archived 2026-06-25. All three acceptance criteria are met by the
Current State above:

- Zero-config browser sign-in in `wallfacer run` (`resolveAuthConfig` public
  client), silent-available on first run, fully usable signed-out.
- Shared latere-ui console chrome via thin wrappers (`AccountControl.vue`,
  `Sidebar.vue`) over `AccountMenu` / `ConsoleSidebar`, `brandTheme: 'wallfacer'`.
- `docs/guide/configuration.md` documents the `AUTH_*` defaults, public client,
  precedence, and how to point at a different auth service.

No remaining wallfacer-side work. Future console-shell improvements land by
bumping the `latere-ui` pin, not by reopening this spec. Org-scoped behavior
(shared boards, RBAC) is owned by `multi-user-collaboration`.
