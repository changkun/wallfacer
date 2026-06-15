---
title: Collapse spec dispatch/undispatch/archive/unarchive into one transition endpoint
status: archived
depends_on:
  - specs/local/backend-redundancy-cleanup.md
  - specs/local/vue-frontend-migration.md
affects:
  - internal/apicontract/routes.go
  - internal/handler/specs.go
  - internal/handler/specs_dispatch.go
  - internal/cli/server.go
  - ui/js/generated/routes.js
  - frontend/src/
  - ui/js/
effort: medium
created: 2026-06-01
updated: 2026-06-15
author: changkun
dispatched_task_id: null
---


# Collapse spec dispatch/undispatch/archive/unarchive into one transition endpoint

Today the spec tree fires four verb-specific POSTs:

- `POST /api/specs/dispatch`
- `POST /api/specs/undispatch`
- `POST /api/specs/archive`
- `POST /api/specs/unarchive`

Each takes a `paths` array. The internal logic differs (dispatch
creates board tasks; archive walks descendants and writes a single
revertable git commit; undispatch cancels linked tasks; unarchive
reverts the archive commit). The collapse is a **wrapper**
consolidation, not a body merge.

## Target shape

```
POST /api/specs/transition
Body: {"paths": [...], "action": "dispatch" | "undispatch" | "archive" | "unarchive"}
```

The response envelope already differs per action today (dispatch and
undispatch return per-spec arrays; archive returns a single
`specTransitionResponse`). Keep that per-action shape and document it
in the contract — the client already branches on action.

## Backend changes

1. Add `POST /api/specs/transition` to
   `internal/apicontract/routes.go`. Remove the four originals.
2. Add a thin dispatcher in `internal/handler/specs_dispatch.go`
   (probably) that switches on `action` and delegates to the existing
   four implementations. The implementations stay where they are.
3. Update `internal/cli/server.go` wiring (drop four `bodyLimits`
   entries, add one).
4. Regenerate `ui/js/generated/routes.js`.

## Frontend changes

Search both `ui/js/` and `frontend/src/` for `/api/specs/dispatch`,
`/api/specs/undispatch`, `/api/specs/archive`, `/api/specs/unarchive`
and migrate each call site to the new endpoint with the appropriate
`action`. Update tests under `ui/js/tests/` and `frontend/src/`.

## Acceptance

- All four POST routes removed from `apicontract.Routes`.
- Every frontend call site uses the new endpoint with the right
  `action`.
- Behaviour unchanged: archive still produces a revertable git commit;
  dispatch still creates tasks; etc.

## Out of scope

- The internal implementations of the four actions — only the HTTP
  edge moves.
- `internal/handler/specs.go:collectArchiveTargets` and other archive
  helpers stay in place.
