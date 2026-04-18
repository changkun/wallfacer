---
title: Routines HTTP API
status: complete
depends_on:
  - specs/local/routine-tasks/task-kind-and-fields.md
affects:
  - internal/apicontract/routes.go
  - internal/handler/routines.go
  - ui/js/generated/routes.js
  - docs/guide/board-and-tasks.md
  - CLAUDE.md
  - AGENTS.md
effort: medium
created: 2026-04-18
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Routines HTTP API

## Goal

Add dedicated `/api/routines` endpoints for creating, listing, updating, and
triggering routine cards. These are the user-facing CRUD surface that the
new UI will call. The engine integration (actually firing routines) is a
separate later task — this one ships the API shape first.

## What to do

1. In `internal/apicontract/routes.go`, register:
   - `GET /api/routines`
   - `POST /api/routines`
   - `PATCH /api/routines/{id}/schedule`
   - `POST /api/routines/{id}/trigger`

   Regenerate `ui/js/generated/routes.js` via `make api-contract`.

2. Create `internal/handler/routines.go` with:

   ```go
   type RoutineResponse struct {
       ID                     uuid.UUID  `json:"id"`
       Prompt                 string     `json:"prompt"`
       Goal                   string     `json:"goal,omitempty"`
       Tags                   []string   `json:"tags,omitempty"`
       RoutineIntervalSeconds int        `json:"routine_interval_seconds"`
       RoutineEnabled         bool       `json:"routine_enabled"`
       RoutineNextRun         *time.Time `json:"routine_next_run,omitempty"`
       RoutineLastFiredAt     *time.Time `json:"routine_last_fired_at,omitempty"`
       RoutineSpawnKind       TaskKind   `json:"routine_spawn_kind,omitempty"`
       CreatedAt              time.Time  `json:"created_at"`
       UpdatedAt              time.Time  `json:"updated_at"`
   }

   func (h *Handler) ListRoutines(w, r)       // filters ListTasks by Kind
   func (h *Handler) CreateRoutine(w, r)      // wraps CreateTaskWithOptions
   func (h *Handler) UpdateRoutineSchedule(w, r) // PATCH schedule
   func (h *Handler) TriggerRoutine(w, r)     // POST trigger
   ```

   - `CreateRoutine` body:
     `{prompt, goal?, interval_minutes, spawn_kind?, enabled?, timeout?,
       tags?}`. Converts `interval_minutes` to seconds; requires
     `interval_minutes >= 1`; defaults `enabled=true`, `spawn_kind=""`.
     Returns `201` with the `RoutineResponse`.
   - `UpdateRoutineSchedule` body: `{interval_minutes?, enabled?}`. Unset
     fields are left unchanged. Writes via `UpdateRoutineSchedule` /
     `UpdateRoutineEnabled` from the previous task.
   - `TriggerRoutine` for v1 returns `202 Accepted` and writes a
     `system:triggered` event on the routine card. (Actual spawn happens
     after engine integration.)
   - `ListRoutines` returns all `Kind == TaskKindRoutine` tasks, sorted by
     `created_at` ascending.

3. Register handlers in `internal/handler/handler.go` alongside the other
   route wiring.

4. Update docs:
   - `docs/guide/board-and-tasks.md` — add a "Routine tasks" subsection
     under Advanced Topics explaining the concept, how to create one via
     the UI (stub — UI in a later task), and that interval is in minutes.
   - `CLAUDE.md` and `AGENTS.md` — add the new routes under the "Tasks"
     API section.

## Tests

- `internal/handler/routines_test.go`:
  - `TestCreateRoutine_Valid` — POST minimal body, assert `201`, routine
    card appears in `ListTasks` with `Kind=routine`, `RoutineEnabled=true`,
    `RoutineIntervalSeconds=<minutes>*60`.
  - `TestCreateRoutine_SpawnKindIdeaAgent` — spawn_kind="idea-agent"
    accepted; stored on the card.
  - `TestCreateRoutine_RejectsTooShortInterval` — interval_minutes=0
    returns `422`.
  - `TestCreateRoutine_RejectsUnknownSpawnKind` — returns `422`.
  - `TestListRoutines_FiltersByKind` — seed a routine card and a normal
    task; GET returns only the routine.
  - `TestUpdateRoutineSchedule_ChangesInterval` — PATCH interval, assert
    stored.
  - `TestUpdateRoutineSchedule_TogglesEnabled` — PATCH `enabled=false`,
    assert stored and `RoutineNextRun` is cleared (engine-less expectation
    in this task: the handler explicitly nils `RoutineNextRun` on disable).
  - `TestTriggerRoutine_WritesEvent` — POST trigger; assert `202`, event
    log contains a `system:triggered` event. (No spawn yet; that lands
    with engine integration.)
  - `TestUpdateRoutineSchedule_UnknownID` — returns `404`.

## Boundaries

- No scheduler engine yet; `TriggerRoutine` does not actually spawn
  instance tasks in this task. It writes the event so the engine-
  integration task can hook it up later.
- No UI code.
- Do not migrate ideation or touch `/api/ideate`.
- Do not add DELETE — deletion is handled by the existing
  `DELETE /api/tasks/{id}`.
