---
title: "Add taskWorker Type"
status: archived
depends_on: []
affects:
  - internal/sandbox/worker.go
effort: medium
created: 2026-03-27
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---


# Task 1: Add taskWorker Type

## Goal

Create the `taskWorker` type that manages a long-lived per-task container
and can execute commands inside it via `podman exec`.

## What to do

1. Create `internal/sandbox/worker.go` with:

   ```go
   type taskWorker struct {
       mu            sync.Mutex
       command       string        // "podman" or "docker"
       containerName string        // e.g. "wallfacer-task-abcd1234"
       createArgs    []string      // args for podman create (computed once)
       alive         bool          // true when container is running
   }
   ```

2. Implement `ensureRunning(ctx context.Context) error`:
   - If `alive`, run `podman inspect --format '{{.State.Running}}' <name>`
     to verify. If not running, set `alive = false` and fall through.
   - Run `podman rm -f <name>` to clean up any leftover.
   - Run `podman create --name <name> --entrypoint '["sleep","infinity"]' <createArgs...>`
   - Run `podman start <name>`
   - Set `alive = true`

3. Implement `exec(ctx context.Context, cmd []string) (Handle, error)`:
   - Call `ensureRunning()` first.
   - Build args: `exec <name> <cmd...>`
   - Launch via `os/exec.CommandContext` (same pattern as `localHandle`).
   - Return a `localHandle` wrapping the exec process (stdout/stderr pipes,
     Wait, Kill all work the same as ephemeral).

4. Implement `stop()`:
   - Run `podman rm -f <name>` (ignore errors).
   - Set `alive = false`.

## Tests

- `TestTaskWorkerEnsureRunning` — create a worker, verify container starts
  (requires podman/docker; skip in CI if unavailable).
- `TestTaskWorkerExec` — exec a simple command (`echo hello`), verify stdout.
- `TestTaskWorkerStop` — stop a running worker, verify container removed.
- `TestTaskWorkerExecAfterStop` — stop then exec, verify worker auto-recovers.

## Boundaries

- Do NOT modify `LocalBackend` yet (that is task 3).
- Do NOT add feature flags yet (that is task 3).
- The `createArgs` are passed in at construction, not computed here.
