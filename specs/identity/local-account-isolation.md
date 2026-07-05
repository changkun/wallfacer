---
title: Local Per-Account / Per-Org Project & Task Isolation
status: complete
depends_on: []
affects:
  - internal/handler/config.go
  - internal/handler/workspace_crud.go
  - internal/handler/tasks.go
  - internal/handler/principal.go
  - internal/cli/server.go
  - internal/store/principal.go
  - internal/workspace/groups.go
  - internal/handler/device_auth.go
effort: large
created: 2026-07-05
updated: 2026-07-05
author: changkun
dispatched_task_id: null
---

# Local Per-Account / Per-Org Project & Task Isolation

## Overview

Make a signed-in local `wallfacer run` scope projects (workspaces) and tasks to the active
personal account or organization, instead of showing everything regardless of who is signed
in. Today isolation is deliberately cloud-only, so once local device-code sign-in became
functional a signed-in user saw every project mixed across accounts and orgs — the reported
breakage. This spec extends the already-shipped principal-visibility plumbing to local mode
and adds a safe owner-adoption migration so existing (owner-less) projects survive the
switch, then re-enables device sign-in (currently reverted) as one coherent change.

## Motivation and the design reversal

The current design is intentional: local single-user runs never isolate. `internal/cli/server.go`
(the `SetCloudMode` comment) and `internal/handler/handler.go:174-179` state a local run "must
never hide a user's own workspace just because their session carries a different org label,"
and `workspace_visibility_test.go:51-57` guards exactly that. The premise was that local mode
has no principal — `multi-user-collaboration.md:113-121` frames local as "auth service not
configured → anonymous."

That premise no longer holds: local mode wires the public `wallfacer` OIDC client
(`server.go` `SetAuth`), and [local-device-signin](local-device-signin.md) made sign-in real,
so a local session now carries a principal (`Sub`, possibly `OrgID`). The user has an account
and organizations and expects the board to reflect the active one. This spec reverses the
"local never isolates" decision **for signed-in local sessions only**: anonymous local use is
unchanged (no principal → see everything).

## Current State

Two isolation layers exist and disagree — the bug is the disagreement.

- **Layer 1 — workspace/project visibility: cloud-only (OFF locally).** Three gates:
  - `internal/handler/config.go:104` `workspaceVisibleTo`: `if !h.cloudMode { return true }`.
  - `internal/handler/config.go:176` `buildConfigResponse`: filters via `WorkspacesForPrincipal`
    only under `h.cloudMode && ok && c != nil`.
  - `internal/handler/workspace_crud.go:69` `visibilityPrincipal`: `if !h.cloudMode { return nil }`
    → `ListWorkspaces` / `workspaceVisibleByID` filter nothing locally.
- **Layer 2 — task visibility: principal-gated (already ON whenever a principal exists).**
  `internal/handler/tasks.go:140` and `graph.go:27` call `store.TasksForPrincipal(...,
  principalFromRequest(r), ...)`; `internal/handler/principal.go:20` returns a principal
  whenever the session cookie decodes — no `cloudMode` guard. Rules in
  `internal/store/principal.go` `principalSeesTask`: nil → all; org view (`OrgID!=""`) →
  strictly `t.OrgID==p.OrgID` (personal + legacy hidden); personal view (`OrgID==""`) →
  `t.OrgID==""` and (`CreatedBy==""` or `CreatedBy==p.Sub`).
- **Owner stamping** already happens at creation when a principal is present:
  `tasks.go:237,546` stamp `CreatedBy`/`OrgID` on new tasks; `workspace_crud.go:72,81`
  (`ownerPrincipal` → `CreateWorkspace`) stamp new workspaces. Pre-existing records are all
  legacy (`CreatedBy=""`, `OrgID=""`).
- **Visibility matrix** `internal/workspace/groups.go:190-226` `WorkspacesForPrincipal`: nil →
  all; personal → own + legacy (no-owner); org X → strictly `OrgID=="X"`, excluding personal
  and legacy.
- **`RequirePrincipalMiddleware`** (`handler.go:664`) uses `principalFromRequest` for an
  identity-presence auth gate (spec comments), unrelated to filtering — must stay unchanged.
- **Device sign-in is currently reverted** (`794602ef` reverted `ab280b01`): the wiring block
  in `server.go` is gone, so `/api/auth/device/*` answers 503 and the SPA falls back to
  `/login`. It was reverted because login without this isolation is the broken state.

## Architecture

Two moves, then re-enable sign-in:

1. **Unify the gate.** Replace the "isolate iff `cloudMode`" rule with "isolate iff a principal
   is present" across both layers. A single helper (e.g. `h.scopedPrincipal(r)`) returns the
   request principal whenever one exists, regardless of `cloudMode`; anonymous → nil → the
   existing see-everything fallback. Layer 1's three `cloudMode` gates and Layer 2's direct
   `principalFromRequest` calls both route through it, so workspaces and tasks isolate
   identically. `cloudMode` keeps its other roles (forced login, workspace data-path scoping)
   but no longer gates visibility.

2. **Adopt legacy data on first sign-in (the landmine).** Before this change every local
   project/task is legacy. Under the org matrix, switching to an org hides all legacy — correct
   isolation, but alarming if a session defaults to an org, and it leaves multiple local
   accounts sharing the same legacy pool (legacy = "deployment-shared, visible to any signed-in
   user"). An idempotent adoption stamps existing legacy workspaces and tasks with the first
   local signer's `Sub` (personal, `OrgID=""`) so they become that account's personal projects:
   visible in personal view, correctly excluded from org views, and invisible to a different
   local account. Runs once (guarded by a marker); a no-op when there is nothing legacy.

3. **Default local sign-in to personal view** so adoption + personal semantics keep existing
   projects visible immediately after login; org is opt-in via the existing switcher.

4. **Re-enable device sign-in** by re-applying the reverted `server.go` wiring.

```mermaid
flowchart TD
  R[request] --> P{principal in session?}
  P -- no --> ALL[see everything（anonymous, unchanged）]
  P -- yes --> S[h.scopedPrincipal]
  S --> W[WorkspacesForPrincipal]
  S --> T[TasksForPrincipal]
  W --> V{OrgID?}
  T --> V
  V -- "personal（\"\")" --> OWN[own + adopted-legacy]
  V -- "org X" --> ORG[strictly org X]
```

## Components

### Unified scoped-principal gate

- `internal/handler/principal.go` — add `func (h *Handler) scopedPrincipal(r *http.Request)
  *store.Principal` returning `principalFromRequest(r)` unconditionally (principal-presence),
  with a doc note that anonymous → nil → see-everything and that `cloudMode` no longer gates
  visibility. Keep the free `principalFromRequest` for `RequirePrincipalMiddleware`.
- Layer 1: `config.go:104` `workspaceVisibleTo`, `config.go:176` `buildConfigResponse`,
  `workspace_crud.go:69` `visibilityPrincipal` — drop the `cloudMode` short-circuits; filter
  whenever `scopedPrincipal` is non-nil. Preserve the cross-org active-workspace reset
  (`config.go:183`).
- Layer 2: `tasks.go:140`, `graph.go:27` — route through `scopedPrincipal` (behavior
  unchanged when a principal exists; the point is the shared definition).

### Legacy owner-adoption migration

- New: an idempotent adoption that, on first local sign-in with a principal, stamps every
  legacy workspace (`internal/workspace` group with empty `CreatedBy`/`OrgID`) and every legacy
  task (`store` records) with `CreatedBy=<sub>`, `OrgID=""`. Guarded by a persisted marker
  (e.g. under the config dir, or an `adopted_by` sentinel) so it runs once and never re-adopts
  after a second account signs in. New workspaces/tasks are already stamped at creation.
- Placement: triggered from the sign-in completion path (device `poll` `done`, and the browser
  `/callback`) or lazily on the first authenticated request; must be safe under concurrency and
  a no-op when nothing is legacy. Preserve the workspace `DataKey` (storage path is per-
  workspace, independent of owner — `groups.go:24-28` — so adoption moves zero bytes).

### Default-personal local session

- `internal/handler/device_auth.go` start (`:141`) already sends `org_id` only when
  `body.Personal`/`body.OrgID` is set; confirm a bare local sign-in yields `OrgID==""`
  (personal). If the token still carries a default org, coerce the local session to personal
  on mint so existing projects stay visible until the user picks an org.

### Re-enable device sign-in

- `internal/cli/server.go` — re-apply the reverted wiring (`h.SetDeviceAuth(&handler.DeviceAuth{
  OIDC: authClient, Store: coordTokenStore})`, local-mode only). Land it in the same change as
  the isolation + adoption so login is never enabled without them.

## Data Flow

Anonymous local request → `scopedPrincipal` nil → every workspace and task returned (unchanged).
Signed-in personal → own + adopted-legacy projects/tasks. Switch to org X (existing switcher,
`/api/me/switch-org`) → strictly org-X projects/tasks; personal/legacy hidden until switching
back. First-ever local sign-in triggers one adoption pass so legacy data lands in that account's
personal space.

## Error Handling

- Adoption failure must not block sign-in: log and continue (the user still authenticates; the
  board falls back to legacy-visible personal view). Idempotent marker prevents partial re-runs.
- Anonymous parity: with no principal, every gate returns everything — no new 401s, no hidden
  projects, matching today's self-hosted anonymous behavior.
- `RequirePrincipalMiddleware` semantics unchanged (identity-presence for spec comments).

## Testing Strategy

- **Store** (`principal_test.go`): existing matrix stays green; add a case that a personal
  principal sees adopted-legacy while an org principal does not.
- **Handler** (`config`/`workspace_crud`/`tasks` tests): with `cloudMode=false` and a signed-in
  personal principal, workspaces AND tasks are scoped (the regression: previously all visible);
  a second local account does not see the first's adopted projects; anonymous sees everything.
  Reconcile `workspace_visibility_test.go:51-57` — its "local never hides" assertion becomes
  "anonymous local never hides; signed-in local isolates."
- **Adoption**: idempotent (second run no-ops), stamps only legacy records, moves no `DataKey`,
  survives concurrent first requests, no-op on an empty/first-run instance.
- **End-to-end**: sign in locally → existing projects still visible (personal) → switch to org
  → only org projects → switch back → personal projects return. Re-run the
  [local-device-signin](local-device-signin.md) suite to confirm re-enabled sign-in.

## Outcome

**Summary.** Shipped much smaller than specced after verifying two facts against reality: the
migration and coercion the spec centered on are unnecessary. The fix is the gate unification
plus re-enabling sign-in, with one refinement (the active workspace is scoped differently from
the project list). Commits: `bccd81bc` (unify), `85e6ce6e` (re-enable device sign-in),
`6ed87396` (active-workspace split). Drift: Moderate — the goal (signed-in local isolates per
account/org) is met, but via a smaller mechanism than the spec's design.

**What the implementation actually needed (and the spec over-scoped).**
- **No owner-adoption migration.** Verified from source that personal view already includes
  legacy no-owner records: `WorkspacesForPrincipal` (`groups.go:221`) and `principalSeesTask`
  (`store/principal.go:87`) both return legacy (`CreatedBy==""`, `OrgID==""`) entries for a
  personal (`OrgID==""`) caller. So existing projects/tasks stay visible in personal view with
  no data mutation; org view correctly excludes them. The "existing projects vanish" landmine
  only fires if the session defaults to an *org* view — which it does not (next point). The
  adoption migration is therefore deferred; it is only needed to separate legacy data across
  *multiple* local accounts on one machine, which is not the reported need.
- **No personal-coercion.** Decoded the user's real persisted device token
  (`~/.config/latere/token.json`): it carries **no org claim** (`OrgID==""`), so a bare local
  sign-in is already personal. No mint-time coercion needed.
- **No `scopedPrincipal` helper / Layer-2 change.** Layer 2 (tasks) already gated on
  principal-presence (`principalFromRequest`, no `cloudMode`). Only Layer 1 (workspaces) was
  cloud-gated. The fix was to drop the three Layer-1 `cloudMode` short-circuits so both layers
  share one rule; no new helper was warranted.

**What shipped.**
- `config.go` `workspaceVisibleTo` + `buildConfigResponse` and `workspace_crud.go`
  `visibilityPrincipal`: filter by principal presence, not `cloudMode`. Anonymous → see all;
  signed-in → personal/org scope (local and cloud alike). `handler.go` `cloudMode` doc updated
  (it no longer gates visibility; still gates forced login + data-path scoping).
- `server.go`: device-code sign-in re-enabled (reverted revert).
- **List vs active split** (`6ed87396`): the project *list* filters strictly
  (`WorkspacesForPrincipal`); the *active* workspace is dropped only on a genuine cross-org
  carry-over (stamped to a different org than the caller), via the shared
  `activeWorkspaceCrossOrg` helper used by both `buildConfigResponse` and `workspaceVisibleTo`.
  Without this, an org caller who activated a legacy/unowned project lost their board. Legacy
  and same-org active workspaces stay usable — activating a project implicitly claims it.
- Test: `workspace_visibility_test.go` rewritten into the reproducer — a signed-in local
  personal/other-org caller no longer sees an org-stamped workspace (403 on mutation);
  anonymous still sees everything; the owning org sees it. `TestArchiveSpec_ForbiddenForHiddenWorkspace`
  now asserts isolation *without* `cloudMode`.

**Known caveat / follow-up.** Org switching still uses the browser `/login?org_id=` redirect
(`orgs.go:158` `doOrgSwitch`), not the device flow. After a device sign-in the browser holds
an auth-service SSO session, so the switch is silent when the local callback `redirect_uri` is
registered (default port). If a follow-up wants fully redirect-free org switching (and
multi-account legacy separation), that is the deferred adoption + a device-code switch path —
tracked here, not built.
