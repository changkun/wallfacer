---
title: Collapse pure state-transition task actions into PATCH
status: drafted
depends_on:
  - specs/local/backend-redundancy-cleanup.md
  - specs/local/vue-frontend-migration.md
affects:
  - internal/apicontract/routes.go
  - internal/handler/execute.go
  - internal/handler/tasks.go
  - internal/cli/server.go
  - ui/js/generated/routes.js
  - frontend/src/
  - ui/js/
effort: medium
created: 2026-06-01
updated: 2026-06-01
author: changkun
dispatched_task_id: null
---

# Collapse pure state-transition task actions into PATCH

Today there are eight dedicated action endpoints, four of which are
pure state transitions that `PATCH /api/tasks/{id}` already covers:

| Endpoint | Action | Collapses? |
|---|---|---|
| `POST /api/tasks/{id}/cancel` | sets `cancelled`, cleans worktrees | yes → `PATCH ... {status:"cancelled"}` |
| `POST /api/tasks/{id}/archive` | flips archived flag | yes → `PATCH ... {archived:true}` |
| `POST /api/tasks/{id}/unarchive` | flips archived flag | yes → `PATCH ... {archived:false}` |
| `POST /api/tasks/{id}/restore` | undelete tombstone | yes → `PATCH ... {deleted:false}` |
| `POST /api/tasks/{id}/done` (CompleteTask) | sets `done` + auto-commit | no — non-trivial side effect |
| `POST /api/tasks/{id}/resume` | continues failed/waiting task | no — runner restart |
| `POST /api/tasks/{id}/sync` | rebase worktree onto main | no — git operation |
| `POST /api/tasks/{id}/test` | run test agent | no — agent launch |

## Scope

1. Extend the PATCH body decoder in
   `internal/handler/tasks.go:UpdateTask` to accept
   `archived *bool` and a `cancelled` status transition path that
   does the worktree cleanup currently in `CancelTask`. (Status =
   cancelled is already part of the lifecycle state machine; the only
   side effect is the worktree cleanup, which can move into
   PATCH's status-change branch.)
2. Same for `deleted: false` (restore) — move the body of
   `RestoreTask` into PATCH.
3. Remove the four routes (`cancel`, `archive`, `unarchive`,
   `restore`) from `internal/apicontract/routes.go` and delete the
   four handler functions from `internal/handler/execute.go` /
   `tasks.go`.
4. Wiring updates in `internal/cli/server.go`. Regenerate
   `ui/js/generated/routes.js`.
5. Frontend call-site migration in both `ui/js/` and `frontend/src/`,
   plus tests.

Optionally also: rename the four kept POSTs to make their side effect
explicit. E.g. `done` → `commit-and-done`. Bundle that rename here or
defer.

## Cross-cutting helper

Pass-1 notes flagged a `transitionTask(id, newStatus, opts)` helper
that would centralise the diff-cache-invalidate + event-log + thread-
cascade ritual repeated across all eight current handlers and the
PATCH paths. Land it as part of this spec since PATCH is about to
absorb four more of those rituals.

## Acceptance

- Four POST endpoints removed.
- PATCH handler covers archive/unarchive/cancel/restore, with the
  same cascades (thread archive, routine child cancel, etc.) as the
  old dedicated handlers.
- The four kept POSTs (`done`, `resume`, `sync`, `test`) work
  unchanged.
- Frontend call sites migrated; tests pass.

## Out of scope

- The four side-effect POSTs that don't fold into PATCH.
- The `archive-done` bulk endpoint (operates on a set, not one task) —
  stays as-is.
