---
title: Unify oversight + test-oversight with a phase query param
status: drafted
depends_on:
  - specs/local/backend-redundancy-cleanup.md
  - specs/local/vue-frontend-migration.md
affects:
  - internal/apicontract/routes.go
  - internal/handler/oversight.go
  - internal/cli/server.go
  - ui/js/generated/routes.js
  - frontend/src/
  - ui/js/
effort: small
created: 2026-06-01
updated: 2026-06-01
author: changkun
dispatched_task_id: null
---

# Unify oversight + test-oversight with a phase query param

Today:

- `GET /api/tasks/{id}/oversight`
- `GET /api/tasks/{id}/oversight/test`

The two handlers in `internal/handler/oversight.go:22,43` differ only
in `store.GetOversight` vs `store.GetTestOversight`. The response
shape is *almost* identical — `GetOversight` wraps with a
precomputed `PhaseCount`; `GetTestOversight` doesn't.

## Target shape

```
GET /api/tasks/{id}/oversight?phase=impl   (default)
GET /api/tasks/{id}/oversight?phase=test
```

Pick one response shape and use it for both phases (probably the
`PhaseCount`-wrapped one — adding a numeric field is cheaper than
the client computing it). The same JSON envelope flows back for both
phases.

## Backend changes

1. Remove `GET /api/tasks/{id}/oversight/test` from
   `internal/apicontract/routes.go`.
2. Merge the two handler functions into one
   `GetOversight(w, r, id)` that reads `?phase=` (default `impl`),
   dispatches to the right store call, and writes the unified
   response.
3. Update `internal/cli/server.go` wiring (drop one handler entry).
4. Regenerate `ui/js/generated/routes.js`.

## Frontend changes

Find all call sites of `/api/tasks/.../oversight/test` in both
`ui/js/` and `frontend/src/` and migrate them to the base URL with
`?phase=test`. Update tests.

## Acceptance

- The `/oversight/test` route is gone.
- Both phases return the same JSON envelope; client decodes
  unchanged for `phase=impl` and gains the extra `phase_count` field
  for `phase=test`.
- Existing handler tests cover both branches via the new `?phase=`
  param.

## Out of scope

- The async generation that produces the underlying summaries — both
  phases use the same `runner.GenerateOversight` and `runner.Test`
  paths, unchanged.
