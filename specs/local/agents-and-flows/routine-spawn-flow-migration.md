---
title: Routine.SpawnFlow replaces RoutineSpawnKind
status: complete
depends_on:
  - specs/local/agents-and-flows/runner-flow-integration.md
affects:
  - internal/store/models.go
  - internal/store/tasks_update.go
  - internal/handler/routines.go
  - internal/handler/routines_engine.go
  - ui/js/routines.js
effort: small
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Routine.SpawnFlow replaces RoutineSpawnKind

## Goal

Align the routine primitive with the Flow concept by renaming
`RoutineSpawnKind` → `RoutineSpawnFlow` and threading a flow slug
through the routine's fire path. Back-compat: routine records
persisted with `RoutineSpawnKind` continue to resolve via the
legacy-kind→flow mapper shipped in the task-flow-field task, so no
data migration is needed for existing stores.

## What to do

1. `internal/store/models.go`:
   - Add `RoutineSpawnFlow string` on `Task` (`json:"routine_spawn_flow,omitempty"`).
   - Keep `RoutineSpawnKind` for back-compat; mark it deprecated
     via a doc comment. Add a helper
     `(*Task) ResolvedRoutineFlow(reg *flow.Registry) string` that
     returns `RoutineSpawnFlow` when non-empty, else maps
     `RoutineSpawnKind` via the legacy resolver, else defaults to
     `implement`.

2. `internal/store/tasks_update.go`:
   - `UpdateRoutineSpawnFlow(ctx, id uuid.UUID, slug string)
     error`. The existing `UpdateRoutineSpawnKind` stays and
     updates only the legacy field.

3. `internal/handler/routines.go`:
   - `CreateRoutine` accepts a `spawn_flow` body field; maps to
     `TaskCreateOptions.RoutineSpawnFlow`.
   - When both `spawn_flow` and `spawn_kind` are present,
     `spawn_flow` wins.
   - Response carries both fields; the legacy one is populated
     from the resolved flow's `SpawnKind` for older UIs that read
     it.

4. `internal/handler/routines_engine.go`:
   - `fireRoutine` reads `task.ResolvedRoutineFlow(registry)` and
     creates the instance task with `FlowID: <slug>`. The runner
     then picks up the flow via its normal dispatch.

5. `ui/js/routines.js`:
   - Composer's routine creation (from `composer-flow-picker`
     task) sends `spawn_flow: <slug>` when the user ticks
     "Repeat on a schedule". Remove the temporary `spawn_kind`
     fallback wired in the composer task.

## Tests

- `internal/store/models_test.go`:
  - `TestResolvedRoutineFlow_PrefersSpawnFlow`.
  - `TestResolvedRoutineFlow_LegacyIdeaAgentMapsToBrainstorm`.
- `internal/handler/routines_engine_test.go`:
  - `TestFireRoutine_SpawnsInstanceWithFlowID`.
- `ui/js/tests/routines.test.js`:
  - Composer test: POST body carries `spawn_flow` not
    `spawn_kind` when creating a routine with the new composer.

## Boundaries

- Do NOT remove `RoutineSpawnKind` — legacy records still use
  it. The retirement happens in a later deprecation pass.
- Do NOT touch the routine schedule machinery.
- Do NOT add new routine-related UI controls beyond what the
  composer-flow-picker task already wired.
