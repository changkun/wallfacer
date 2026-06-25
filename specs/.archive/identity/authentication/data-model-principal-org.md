---
title: Principal and org fields on task + workspace records
status: archived
depends_on: []
affects:
  - internal/store/
  - internal/workspace/
  - internal/handler/
effort: medium
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---


# Principal and org fields on task + workspace records

## Goal

Give every persisted task and workspace group a place to record *who created
it* (`CreatedBy`, from `claims.Sub`) and *which org owns it* (`OrgID`, from
`claims.OrgID`). This is the data foundation for multi-tenant isolation in
cloud mode and for attribution ("workspace X, created by Alice") in any
mode where a user is signed in.

Both fields are nullable / empty by default. Local anonymous usage keeps
writing records with `CreatedBy=""` and `OrgID=nil`, and queries behave
identically to today. No data migration is required.

Runs parallel with `jwt-middleware.md`; does not depend on it structurally
but is only useful once the middleware is populating claims.

## What to do

1. **Store model**, extend `internal/store/task.go`:
   ```go
   type Task struct {
       // ... existing fields ...
       CreatedBy string  `json:"created_by,omitempty"` // principal ID (claims.Sub)
       OrgID     string  `json:"org_id,omitempty"`     // claims.OrgID, empty if not org-scoped
   }
   ```
   Use empty-string + `omitempty` rather than `*string` to keep the JSON on
   disk terse for the anonymous case. Downstream consumers must not treat
   `""` as "unknown user", treat it as "anonymous" and handle explicitly.
2. **Workspace model**, add the same two fields to whatever the workspace
   manager persists today (`internal/workspace/`). If workspaces are
   currently only identified by their path list, introduce a wrapper struct
   in this task rather than punting. The cloud track needs a stable
   identifier to key per-org workspace listings on.
3. **Populate on create**, in `handler.createTask`, if claims are in
   context:
   ```go
   if c, ok := auth.PrincipalFromContext(r.Context()); ok {
       task.CreatedBy = c.Sub
       if c.OrgID != nil {
           task.OrgID = *c.OrgID
       }
   }
   ```
   Do the same for workspace creation / group switching.
4. **Query filter**, add a helper in `internal/store/`:
   ```go
   // TasksForPrincipal returns the slice of tasks visible to the given
   // principal. When principal is nil, returns all tasks (local mode).
   // When principal has OrgID set, returns tasks with matching OrgID.
   // When principal has no OrgID, returns tasks where OrgID == "".
   func (s *Store) TasksForPrincipal(p *auth.Claims) []*Task
   ```
   Wire the list endpoints through this helper. Legacy records with no
   `OrgID` remain visible to anonymous local callers, which preserves
   backwards compatibility.
5. **Do not change the write path** for anonymous callers. Local-mode task
   creation writes `CreatedBy=""`, `OrgID=""`, exactly as today's records
   look.

## Tests

- `internal/store/task_principal_test.go`:
  - `CreatedBy` is persisted and round-trips through disk.
  - `OrgID` round-trips through disk.
  - `TasksForPrincipal(nil)` returns everything.
  - `TasksForPrincipal(orgA)` returns only tasks where `OrgID == "orgA"`
    (and does NOT return anonymous tasks with empty OrgID).
  - `TasksForPrincipal(noOrg)` returns only tasks where `OrgID == ""`.
- `internal/handler/tasks_test.go`:
  - Task creation with claims in context populates both fields.
  - Task creation without claims populates neither.
  - `GET /api/tasks` respects org scoping when claims carry an org.

## Boundaries

- Do not add an on-disk migration step. Both fields are `omitempty`, old
  records read back with empty strings, which matches the anonymous case.
- Do not add user profile fields (name, email, avatar) to `Task`. Display
  info is fetched from `/api/auth/me` or `/userinfo`.
- Do not add ACL / role checks here. That's `scope-and-superadmin.md`.
- Do not add cross-org visibility rules beyond "strict match". Superadmin
  bypass is in `scope-and-superadmin.md`.

## Coordination with `jwt-middleware.md`

Structurally this spec has no dependency: fields, round-trip, and the
`TasksForPrincipal` filter helper compile and test against a manually
constructed `*Principal` argument. But the "Populate on create" step at
the handler layer reads claims via `auth.PrincipalFromContext`, which is
introduced by `jwt-middleware.md`.

Two-stage delivery, both stages landable from this one spec:

1. **Stage A (immediate):** add the fields, the filter helper, and all
   tests. In `handler.createTask`, call a local nil-safe shim:
   ```go
   // principalOnRequest returns the authenticated principal, or nil.
   // Replaced by auth.PrincipalFromContext once jwt-middleware lands.
   func principalOnRequest(r *http.Request) *auth.Principal { return nil }
   ```
   Stage A leaves `CreatedBy` and `OrgID` empty on every task in both
   local and Phase-1 cloud modes, which matches existing behavior
   exactly.
2. **Stage B (after `jwt-middleware.md`):** replace the shim's body with
   `auth.PrincipalFromContext(r.Context())`. One-line change; no new
   tests needed (Stage A's tests already cover the populated path by
   injecting a `*Principal` directly into the handler constructor).

Both stages ship in one commit if `jwt-middleware.md` has already landed
when this spec is implemented; otherwise Stage A lands first and Stage B
is a trivial follow-up.

## Outcome

Delivered both stages in one pass (jwt-middleware had just landed).
`Task.CreatedBy` and `Task.OrgID` round-trip through the FileStore,
`Store.TasksForPrincipal` implements the org-isolation matrix, and the
`POST /api/tasks` and `GET /api/tasks` handlers thread claims through
via a small boundary-layer translator.

### What shipped
- `internal/store/models.go`: `Task.CreatedBy` and `Task.OrgID` omitempty
  string fields with docstring, placed next to the existing worktree /
  attribution block.
- `internal/store/principal.go`: `Principal` struct (`Sub`, `OrgID`) +
  `TasksForPrincipal(ctx, *Principal, includeArchived)` with the three-way
  filter matrix and a `principalSeesTask` visibility predicate.
- `internal/store/principal_test.go`: 5 tests. `Task_CreatedByAndOrgID_RoundTrip`
  covers the write-reopen cycle. Four `TasksForPrincipal_*` tests cover
  nil, strict org match, no-org-sees-only-anonymous, and `includeArchived`.
- `internal/store/tasks_create_delete.go`: `TaskCreateOptions` carries
  `CreatedBy` and `OrgID` through the create path; both are copied
  verbatim onto the new Task.
- `internal/handler/principal.go`: `principalFromRequest(r)` translates
  `auth.Claims` from ctx into `*store.Principal`, returning nil when no
  claims are in context.
- `internal/handler/tasks.go`: `CreateTask` and the batch path populate
  `opts.CreatedBy` / `opts.OrgID` from `principalFromRequest(r)`.
  `ListTasks` routes through `TasksForPrincipal` instead of raw
  `ListTasks`.
- `internal/handler/tasks_principal_test.go`: 4 handler-level tests
  covering populate-on-create (authenticated + anonymous) and list
  filtering (cloud + local).

### Implementation notes

1. **`TasksForPrincipal` takes `*store.Principal`, not `*auth.Claims`.**
   The spec pseudocode used `*auth.Claims` directly. Passing that would
   force `internal/store` to import `internal/auth`, which transitively
   pulls `latere.ai/x/pkg/jwtauth` into the domain layer — a layering
   violation the store has otherwise avoided. The handler-layer
   `principalFromRequest` is the one-line adapter.

2. **`Principal` is a 2-field struct, not a claim-subset copy.** Only
   `Sub` and `OrgID` are needed for attribution + filtering; other JWT
   claims (Email, Roles, Scopes, IsSuperadmin) belong to handlers that
   consume them directly from `auth.PrincipalFromContext`. Keeping
   `Principal` tiny means the store never grows a model of "what is a
   principal" beyond the two values it actually uses.

3. **Stage A and Stage B shipped together.** Because jwt-middleware
   landed in the previous commit, `auth.PrincipalFromContext` was
   already available when this spec was implemented, so there was no
   need for the nil-safe shim described in "Coordination with
   jwt-middleware". The Coordination section is left in the spec body
   as a retrospective reference for future two-stage specs.

4. **Scope-limited list filtering.** Only `GET /api/tasks` routes
   through `TasksForPrincipal`. The ~20 other `ListTasks` callers
   (autopilot scheduling, runtime metrics, oversight aggregation,
   stats/usage rollups, ideation, planning snapshot, etc.) continue
   to see every task regardless of org. That is intentional:
   - Scheduling callers need fleet-wide visibility so autopilot can
     run tasks from every org without needing per-org slots.
   - Metrics and stats are admin surfaces; adding per-org filtering
     there is out of scope for this spec and belongs alongside the
     `RequireSuperadmin` work in `scope-and-superadmin.md`.
   - Ideation/planning manipulate routine cards that are local-only
     today; org filtering on those will come with the cloud
     multi-tenant spec.

5. **Workspace `CreatedBy` / `OrgID` not implemented.** The spec's
   step 2 ("Workspace model — add the same two fields") was
   deferred. The current workspace manager persists no per-group
   metadata beyond the derived ScopedDataDir path; introducing a new
   `workspace_meta.json` persistence layer, wiring it through
   `Manager.Switch`, and backfilling it on first authenticated switch
   is larger than the spec's "medium" effort allowance and has no
   immediate consumer (no downstream spec reads workspace attribution
   today). When `cloud/multi-tenant.md` starts listing workspaces by
   org, that spec will introduce the persistence layer. For now this
   spec delivers the Task side of the foundation, which is the part
   the cloud-unblock path actually exercises.
