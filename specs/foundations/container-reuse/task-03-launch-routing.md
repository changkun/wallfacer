---
title: "Wire LocalBackend.Launch() to Route Through Per-Task Workers"
status: complete
depends_on:
  - specs/foundations/container-reuse/task-01-task-worker-type.md
  - specs/foundations/container-reuse/task-02-create-args-from-spec.md
affects:
  - internal/sandbox/local.go
effort: medium
created: 2026-03-27
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 3: Wire LocalBackend.Launch() to Route Through Per-Task Workers

## Goal

Modify `LocalBackend.Launch()` to check for a task ID label and route
through a per-task worker when available, falling back to ephemeral
containers otherwise.

## What to do

1. Add worker management fields to `LocalBackend` in
   `internal/sandbox/local.go`:

   ```go
   type LocalBackend struct {
       command           string
       taskWorkers       map[string]*taskWorker // key = task ID string
       taskWorkersMu     sync.Mutex
       enableTaskWorkers bool
   }
   ```

2. Update `NewLocalBackend()` to initialize the map and read
   `WALLFACER_TASK_WORKERS` from the environment (default `true`).

3. Modify `Launch()`:
   - Extract `taskID` from `spec.Labels["wallfacer.task-id"]`.
   - If `taskID != ""` and `enableTaskWorkers`:
     - Look up or create a `taskWorker` for this task ID.
     - Call `worker.exec(ctx, spec.Cmd)` to run the agent command.
   - Otherwise: fall back to `launchEphemeral()` (current behavior).

4. Extract the current `Launch()` body into `launchEphemeral()` for clarity.

5. Add `StopTaskWorker(taskID string)` method for cleanup by the runner:
   - Stops and removes the worker from the map.

6. Add `ShutdownWorkers()` for server shutdown — stops all active workers.

## Tests

- `TestLaunchEphemeralWhenDisabled` — set `enableTaskWorkers=false`,
  verify ephemeral path used.
- `TestLaunchEphemeralWithoutTaskID` — spec with no task-id label, verify
  ephemeral path.
- `TestLaunchCreatesWorker` — spec with task-id, verify worker created.
- `TestLaunchReusesWorker` — two Launch calls with same task-id, verify
  same worker reused (not recreated).
- `TestStopTaskWorker` — stop a worker, verify removed from map.

## Boundaries

- Do NOT change the `Backend` interface.
- Do NOT change the runner — worker cleanup hooks are in task 4.
