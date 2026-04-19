---
title: Principal and org fields on task + workspace records
status: drafted
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
