---
title: Wire scheduler engine into the handler
status: validated
depends_on:
  - specs/local/routine-tasks/engine-package.md
  - specs/local/routine-tasks/routines-api.md
affects:
  - internal/handler/handler.go
  - internal/handler/routines.go
  - internal/handler/routines_engine.go
  - internal/runner/ideate.go
  - internal/prompts/
effort: large
created: 2026-04-18
updated: 2026-04-18
author: changkun
dispatched_task_id: null
---

# Wire scheduler engine into the handler

## Goal

Connect `internal/routine.Engine` to the handler so routine cards actually
fire: the watcher reconciles the engine with current routine cards on every
store change, and when a timer fires the engine spawns an instance task and
runs it via `runner.RunBackground`. After this task, a routine created via
the API spawns instance tasks on schedule end-to-end.

## What to do

1. Add a field to `Handler`: `routineEngine *routine.Engine`.

2. Create `internal/handler/routines_engine.go`:

   ```go
   // StartRoutineEngine initializes the engine and wires it to the watcher
   // so every store change triggers Reconcile.
   func (h *Handler) StartRoutineEngine(ctx context.Context) {
       h.routineEngine = routine.NewEngine(ctx, nil /*real clock*/, h.fireRoutine)
       watcher.Start(ctx, watcher.Config{
           Wake:   h.store,
           Init:   h.reconcileRoutines,
           Action: h.reconcileRoutines,
       })
   }

   // reconcileRoutines diffs the engine registry against current routine
   // cards. Registers new/changed entries, unregisters deleted ones, and
   // writes RoutineNextRun back to the store.
   func (h *Handler) reconcileRoutines(ctx context.Context) {
       tasks, _ := h.store.ListTasks(ctx, false)
       seen := map[uuid.UUID]bool{}
       for _, t := range tasks {
           if t.Kind != store.TaskKindRoutine { continue }
           seen[t.ID] = true
           schedule := routineSchedule(t) // FixedInterval or disabled
           h.routineEngine.Register(t.ID, schedule)
           next := h.routineEngine.NextRuns()[t.ID]
           h.store.UpdateRoutineNextRun(ctx, t.ID, ptrOrNil(next))
       }
       for id := range h.routineEngine.NextRuns() {
           if !seen[id] { h.routineEngine.Unregister(id) }
       }
   }

   // fireRoutine is the Engine's FireFunc: build the instance task, create
   // it, mark it in_progress, and run it.
   func (h *Handler) fireRoutine(ctx context.Context, routineID uuid.UUID) {
       routine, err := h.store.GetTask(ctx, routineID)
       if err != nil || routine.Kind != store.TaskKindRoutine {
           return
       }
       prompt := h.buildRoutinePrompt(ctx, routine)
       opts := store.TaskCreateOptions{
           Prompt:  prompt,
           Kind:    routine.RoutineSpawnKind, // "" or "idea-agent"
           Tags:    append(routine.Tags, "spawned-by:"+routine.ID.String()),
           Timeout: resolveTimeout(routine),
       }
       instance, err := h.store.CreateTaskWithOptions(ctx, opts)
       if err != nil {
           logger.Handler.Warn("routine: create instance", "error", err)
           now := time.Now()
           _ = h.store.UpdateRoutineLastFiredAt(ctx, routineID, &now)
           return
       }
       _ = h.store.UpdateTaskStatus(ctx, instance.ID, store.TaskStatusInProgress)
       now := time.Now()
       _ = h.store.UpdateRoutineLastFiredAt(ctx, routineID, &now)
       h.runner.RunBackground(instance.ID, prompt, "", false)
   }
   ```

3. Prompt building: `buildRoutinePrompt` chooses between:
   - `SpawnKind == TaskKindIdeaAgent` → `runner.BuildIdeationPrompt(active)`
     (reuse existing logic so the ideation migration is a no-op for the
     spawned prompt).
   - Otherwise → the routine card's own `Prompt` verbatim.

4. Hook `TriggerRoutine` into the engine: replace the placeholder from the
   previous task with `h.routineEngine.Trigger(id)`. The engine still
   re-arms the scheduled cycle.

5. Wire `StartRoutineEngine` into `internal/cli/server.go` alongside the
   existing `StartIdeationWatcher` call (both run for now; ideation keeps
   firing via its old path until the migration task lands).

6. E2E script `scripts/e2e-routine-spawn.sh`:
   - `POST /api/routines` with `interval_minutes: 1`.
   - Poll `GET /api/tasks` until a task tagged `spawned-by:<routine-id>`
     appears.
   - Assert routine's `routine_last_fired_at` updated.
   - `PATCH` schedule to disable, confirm no further spawns.
   - Add a `make e2e-routine` target pointing at the script.

## Tests

- `internal/handler/routines_engine_test.go` with an injected fake clock:
  - `TestReconcile_RegistersNewRoutine` — create routine via API, tick
    watcher, assert engine has it.
  - `TestReconcile_UpdatesOnScheduleChange` — PATCH interval, assert
    engine re-armed with new interval.
  - `TestReconcile_UnregistersDeletedRoutine` — DELETE routine card,
    assert engine entry gone.
  - `TestReconcile_WritesNextRunToStore` — after register, routine card
    has `RoutineNextRun` populated.
  - `TestFireRoutine_CreatesInstanceTask` — advance fake clock past
    interval, assert instance task created with `Tags` containing
    `spawned-by:<routine-id>`, promoted to in_progress, runner invoked.
  - `TestFireRoutine_IdeaAgentSpawnKind` — routine with `spawn_kind=
    idea-agent` spawns a `Kind=idea-agent` task with the ideation prompt.
  - `TestFireRoutine_MissingRoutine` — fire a routine that was deleted
    between arm and fire; assert no panic, no instance created.
  - `TestTriggerRoutine_FiresImmediately` — POST trigger spawns an
    instance without waiting.
  - `TestDisabledRoutine_DoesNotFire` — disabled routine, advance clock,
    no spawn.

- Coverage: run the e2e script in CI via `make e2e-routine` (gated behind
  a running server; skipped if no server is up).

## Boundaries

- Do not migrate ideation in this task. `ideate.go`'s existing timer/
  handler code keeps running. The engine runs alongside it for now; the
  ideation migration task replaces the singleton.
- Do not change `/api/ideate` or the ideation config keys.
- Do not build UI; that's a separate task.
- The runner interface stays as-is — no new runner methods for routines.
