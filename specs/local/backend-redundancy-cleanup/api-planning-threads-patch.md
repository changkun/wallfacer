---
title: Collapse planning-thread archive/unarchive/activate into PATCH
status: archived
depends_on:
  - specs/local/backend-redundancy-cleanup.md
  - specs/local/vue-frontend-migration.md
affects:
  - internal/apicontract/routes.go
  - internal/handler/planning_threads.go
  - internal/cli/server.go
  - ui/js/generated/routes.js
  - frontend/src/stores/planning.ts
  - frontend/src/components/plan/PlanningChatPanel.vue
  - ui/js/spec-mode.js
  - ui/js/planning-chat.js
effort: small
created: 2026-06-01
updated: 2026-06-15
author: changkun
dispatched_task_id: null
---


# Collapse planning-thread archive/unarchive/activate into PATCH

Today the planning-thread tab bar fires three verb-specific POSTs:

- `POST /api/planning/threads/{id}/archive`
- `POST /api/planning/threads/{id}/unarchive`
- `POST /api/planning/threads/{id}/activate`

Pass-1 cleanup already collapsed the handler bodies behind a single
`mutatePlanningThread` helper
(`internal/handler/planning_threads.go:253`), so the backend collapse
itself is mechanical — it's the **frontend coordination** that
blocked this in pass 1.

## Target shape

```
PATCH /api/planning/threads/{id}
Body: {"state": "archived"} | {"state": "visible"} | {"state": "active"}
```

`"archived"` and `"visible"` cover archive/unarchive. `"active"`
covers activate (sets the new active thread). The same response shape
(`threadSummary`) flows back, so clients keep their existing decode
path.

## Backend changes

1. Add the PATCH route to `internal/apicontract/routes.go`. Remove
   the three POSTs.
2. Replace the three handler entry points with a single
   `PatchPlanningThread` that decodes `{state}` and dispatches to
   `mutatePlanningThread` with the right `ThreadManager` method.
3. Update `internal/cli/server.go` wiring (drop three `bodyLimits`
   entries, replace with one PATCH wiring).
4. Regenerate `ui/js/generated/routes.js` from the contract.

## Frontend changes

`frontend/src/`:

- `stores/planning.ts:197,212` — change the activate POST to a PATCH.
- `components/plan/PlanningChatPanel.vue:436,475,504` — change all
  three.

`ui/js/` (legacy, may be removed by vue-frontend-migration but still
present at the time of this spec):

- `spec-mode.js:597,617` — change activate.
- `planning-chat.js:336,388,425,456,516` — change all three.

Tests:

- `ui/js/tests/planning-chat-coverage.test.js`
- `ui/js/tests/planning-chat.test.js`
- `ui/js/tests/spec-mode.test.js`

## Acceptance

- All five frontend call sites switched to the PATCH shape and tests
  pass.
- `ui/js/generated/routes.js` no longer references the three POSTs.
- The OpenAPI / `apicontract.Routes` contract has one PATCH and zero
  of the three POSTs.

## Out of scope

If `vue-frontend-migration.md` has already removed `ui/js/` by the
time this spec runs, skip the legacy frontend section entirely.
