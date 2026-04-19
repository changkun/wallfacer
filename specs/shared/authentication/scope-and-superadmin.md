---
title: Superadmin and scope gating for admin API routes
status: validated
depends_on:
  - specs/shared/authentication/jwt-middleware.md
affects:
  - internal/handler/
  - internal/auth/
effort: small
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Superadmin and scope gating for admin API routes

## Goal

Provide the two authorization primitives a cloud deployment needs on day one:

1. **`requireSuperadmin`**, gate sensitive platform operations on the
   `is_superadmin` claim. Applied to `/api/admin/*` routes.
2. **`requireScope(name)`**, gate specific mutating endpoints on the `scp`
   claim array. Scaffolded but not applied anywhere in this task; other
   handlers opt in later.

In local mode both wrappers pass through unchanged, neither is installed
when `cfg.Cloud == false`.

## What to do

1. Add helpers in `internal/auth/authorize.go`:
   ```go
   // RequireSuperadmin returns 403 when claims.IsSuperadmin is false.
   // Returns 401 when there are no claims at all (middleware should
   // typically have produced 401 already, but belt-and-suspenders).
   func RequireSuperadmin(next http.HandlerFunc) http.HandlerFunc

   // RequireScope returns 403 when the claim set does not include
   // the named scope.
   func RequireScope(scope string) func(http.HandlerFunc) http.HandlerFunc
   ```
2. Apply `RequireSuperadmin` to the existing admin endpoint:
   - `POST /api/admin/rebuild-index`
3. Do NOT apply `RequireScope` anywhere in this task. The wrapper exists so
   downstream specs can wire it into their own handlers.
4. Documentation pointer: add a short note to `docs/cloud/README.md`
   describing the two wrappers and how to apply them.

## Tests

- `internal/auth/authorize_test.go`:
  - superadmin claim → 200.
  - non-superadmin claim → 403.
  - no claim in context → 401.
  - `RequireScope("admin:tasks")` with claim containing it → 200;
    without it → 403.
- `internal/handler/admin_test.go`, integration-lite:
  - cloud + superadmin → `/api/admin/rebuild-index` 200.
  - cloud + regular user → 403.
  - local (no claims path) → 200 (unchanged from today).

## Boundaries

- Do not introduce a role matrix or a policy engine. Single-claim checks
  only. Richer RBAC lives in `cloud/multi-user-collaboration.md`.
- Do not apply `RequireScope` to any existing route. Scope assignments
  belong with the teams that own each route.
- Do not log claims on 403. 403 response is "denied"; the audit log is a
  separate spec.
