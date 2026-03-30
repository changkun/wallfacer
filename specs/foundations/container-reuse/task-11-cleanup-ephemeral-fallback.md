---
title: "Remove Ephemeral Fallback and WALLFACER_TASK_WORKERS Flag"
status: complete
depends_on:
  - specs/foundations/container-reuse/task-01-task-worker-type.md
  - specs/foundations/container-reuse/task-02-create-args-from-spec.md
  - specs/foundations/container-reuse/task-03-launch-routing.md
  - specs/foundations/container-reuse/task-04-runner-cleanup-hooks.md
  - specs/foundations/container-reuse/task-05-aux-agents-via-worker.md
  - specs/foundations/container-reuse/task-06-health-check-recovery.md
  - specs/foundations/container-reuse/task-07-lifecycle-metrics.md
  - specs/foundations/container-reuse/task-08-env-config.md
affects:
  - internal/sandbox/local.go
  - internal/envconfig/
effort: small
created: 2026-03-27
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 11: Remove Ephemeral Fallback and WALLFACER_TASK_WORKERS Flag

## Goal

Once per-task workers are proven stable in production, remove the
ephemeral fallback path and the `WALLFACER_TASK_WORKERS` feature flag.
Workers become the only execution strategy for task-scoped containers.

## Prerequisites

Before starting this task, verify:
- Workers have been running in production for at least 2 weeks
- No worker-related bug reports or fallback events in logs
- `wallfacer_container_worker_fallbacks_total` counter is zero or
  negligible

## What to do

1. **Remove `enableTaskWorkers` flag** from `LocalBackend` and
   `LocalBackendConfig`. Workers are always on.

2. **Remove `WALLFACER_TASK_WORKERS`** from:
   - `internal/envconfig/envconfig.go` (field, parsing, known keys)
   - `internal/envconfig/envconfig_test.go` (tests)
   - `CLAUDE.md` (env var reference)
   - `docs/guide/configuration.md` (env var table)

3. **Remove fallback path** in `LocalBackend.Launch()`:
   - Remove the `if err != nil { return b.launchEphemeral(...) }` block
   - Worker errors should propagate directly (the health check in
     `ensureRunning` already handles recovery)
   - Keep `launchEphemeral` for non-task containers (ideation,
     refinement) that don't have a task ID

4. **Remove fallback metric**:
   - Remove `wallfacer_container_worker_fallbacks_total` counter

5. **Simplify `Launch()` routing**:
   ```go
   func (b *LocalBackend) Launch(ctx context.Context, spec ContainerSpec) (Handle, error) {
       if taskID := spec.Labels["wallfacer.task.id"]; taskID != "" {
           return b.launchViaTaskWorker(ctx, spec, taskID)
       }
       return b.launchEphemeral(ctx, spec)
   }
   ```

## Tests

- Remove `TestLaunchEphemeralWhenDisabled` (no longer applicable).
- Verify all remaining tests pass without the feature flag.

## Boundaries

- Do NOT remove `launchEphemeral()` — it's still needed for non-task
  containers (ideation, refinement).
- Do NOT remove `StopTaskWorker` / `ShutdownWorkers` — lifecycle
  management is still needed.
