---
title: "Runner Worker Cleanup on Task Completion"
status: complete
track: foundations
depends_on:
  - specs/foundations/container-reuse/task-03-launch-routing.md
affects:
  - internal/runner/execute.go
  - internal/sandbox/backend.go
effort: medium
created: 2026-03-27
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 4: Runner Worker Cleanup on Task Completion

## Goal

Ensure per-task workers are cleaned up when a task completes, is cancelled,
or fails. The runner must call `backend.StopTaskWorker(taskID)` at the
right lifecycle points.

## What to do

1. Add `StopTaskWorker(taskID string)` to the `Backend` interface in
   `internal/sandbox/backend.go` (or as an optional interface that
   `LocalBackend` implements — check which approach is cleaner).

2. In `internal/runner/execute.go`, after `Run()` finishes (in the deferred
   cleanup or after the commit pipeline), call the backend to stop the
   worker:
   - After successful commit pipeline (task → done)
   - After task → failed
   - After task → cancelled (in the cancel handler)

3. In `internal/runner/runner.go`, on `Shutdown()`, call
   `backend.ShutdownWorkers()` to clean up all active workers.

4. Handle the sync operation: in `SyncWorktrees()`, stop the task's worker
   before rebasing (the worker holds bind mounts to the worktree). The
   worker will be auto-recreated on the next `Launch()` call.

## Tests

- `TestWorkerCleanedUpOnTaskDone` — run a task to completion, verify
  worker is stopped.
- `TestWorkerCleanedUpOnCancel` — cancel a task, verify worker stopped.
- `TestWorkerRecreatedAfterSync` — stop worker for sync, next Launch
  recreates it.
- `TestShutdownStopsAllWorkers` — create 3 workers, call Shutdown,
  verify all stopped.

## Boundaries

- Do NOT change how the runner builds container specs.
- Do NOT add metrics yet (that is task 7).
