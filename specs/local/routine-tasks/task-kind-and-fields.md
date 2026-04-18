---
title: Routine task kind, fields, and store writers
status: validated
depends_on: []
affects:
  - internal/store/models.go
  - internal/store/tasks_update.go
  - internal/store/tasks_create_delete.go
  - internal/handler/tasks_autopilot.go
  - internal/handler/tasks.go
effort: medium
created: 2026-04-18
updated: 2026-04-18
author: changkun
dispatched_task_id: null
---

# Routine task kind, fields, and store writers

## Goal

Add the data-model plumbing for routine cards: a new `TaskKind`, five
optional `Routine*` fields on `Task`, store writers for each, and filtering
of routine cards out of every autopilot path that assumes `Kind == ""`.
After this task, routines can be represented in storage but nothing fires
them yet.

## What to do

1. In `internal/store/models.go`:
   - Add `TaskKindRoutine TaskKind = "routine"` next to the existing
     `TaskKindIdeaAgent` and `TaskKindPlanning` constants.
   - Add fields on `Task` (all `omitempty`; JSON tags in snake_case):
     ```go
     RoutineIntervalSeconds int        `json:"routine_interval_seconds,omitempty"`
     RoutineEnabled         bool       `json:"routine_enabled,omitempty"`
     RoutineNextRun         *time.Time `json:"routine_next_run,omitempty"`
     RoutineLastFiredAt     *time.Time `json:"routine_last_fired_at,omitempty"`
     RoutineSpawnKind       TaskKind   `json:"routine_spawn_kind,omitempty"`
     ```

2. In `internal/store/tasks_update.go` (create if missing — follow the
   existing `tasks_worktree.go` pattern) add writers:
   - `UpdateRoutineSchedule(ctx, id uuid.UUID, intervalSeconds int) error`
   - `UpdateRoutineEnabled(ctx, id uuid.UUID, enabled bool) error`
   - `UpdateRoutineNextRun(ctx, id uuid.UUID, t *time.Time) error`
   - `UpdateRoutineLastFiredAt(ctx, id uuid.UUID, t *time.Time) error`
   - `UpdateRoutineSpawnKind(ctx, id uuid.UUID, kind TaskKind) error`

   Each writer follows the existing pattern: load task, set field, write
   atomically, append a `system` event. Return an "unknown task" error for
   missing IDs.

3. In `internal/handler/tasks.go`:
   - Extend `TaskCreateOptions` plumbing (or the existing create handler) so
     `POST /api/tasks` accepts the new routine fields when `Kind ==
     "routine"`. Validate: `RoutineIntervalSeconds >= 60` (reject "fire
     continuously"), `RoutineSpawnKind` ∈ `{"", "idea-agent"}` (whitelist
     for v1).

4. **Filter routine cards out of autopilot paths.** Grep for every call site
   that iterates tasks for promotion/archiving/dependency resolution and
   skip `Kind == TaskKindRoutine`. At minimum:
   - `internal/handler/tasks_autopilot.go` — `tryAutoPromote`,
     `ensureScheduledPromoteTrigger`, auto-retry, auto-submit, auto-test,
     dep-graph walking. Scheduled promote must not process routine cards
     (they use the engine, not `ScheduledAt`).
   - Any cancel cascade, archive-done handler, stats aggregator that walks
     the task list.

   Add a helper `internal/store/models.go` method:
   `func (t *Task) IsRoutine() bool { return t.Kind == TaskKindRoutine }`

5. **Lifecycle guard.** Routine cards must stay in `TaskStatusBacklog`.
   Reject status transitions on routine cards at the handler layer (return
   `422 unprocessable_entity`). Check this in the `PATCH /api/tasks/{id}`
   status-change path.

## Tests

- `internal/store/routine_test.go`:
  - `TestUpdateRoutineSchedule_Persists` — set interval, reload, assert.
  - `TestUpdateRoutineEnabled_Toggles` — true → false → true.
  - `TestUpdateRoutineNextRun_SetAndClear` — pointer set then nil.
  - `TestUpdateRoutineLastFiredAt_SetAndClear` — same.
  - `TestUpdateRoutineSpawnKind_AllowedKinds` — "", "idea-agent" accepted.
  - `TestUpdateRoutine*_UnknownTask` — returns error.
  - `TestRoutineFields_OmitWhenZero` — round-trip JSON: a non-routine task
    has none of the fields set in its marshaled form.

- `internal/handler/tasks_autopilot_test.go` additions:
  - `TestAutoPromote_SkipsRoutineCards` — a routine card in backlog is not
    promoted to in_progress regardless of capacity.
  - `TestScheduledPromote_SkipsRoutineCards` — routine with a `ScheduledAt`
    in the past is not auto-promoted.

- `internal/handler/tasks_test.go` additions:
  - `TestCreateRoutineTask_ValidatesInterval` — interval < 60 returns 422.
  - `TestCreateRoutineTask_ValidatesSpawnKind` — unknown spawn kind 422.
  - `TestPatchRoutineTask_RejectsStatusChange` — PATCH with `status:
    in_progress` returns 422.

## Boundaries

- No HTTP route at `/api/routines` yet — that's a separate task. This task
  extends only the existing `POST /api/tasks` contract.
- No scheduler engine wiring — nothing fires routines yet.
- No UI changes.
- Do not touch `internal/handler/ideate.go` in this task (ideation keeps
  using `TaskKindIdeaAgent`; its migration is a later task).
- Do not introduce a migration step for existing data — the new fields are
  optional and default-zero on old task files.
