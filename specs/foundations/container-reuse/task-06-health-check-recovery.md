# Task 6: Health Check and Graceful Fallback

**Status:** Done
**Depends on:** Task 3
**Phase:** 3 (Robustness)
**Effort:** Medium

## Goal

Add robust health checking so that dead workers are detected and
recovered transparently. If recovery fails, fall back to ephemeral
containers so the system never breaks.

## What to do

1. In `taskWorker.ensureRunning()`, add container health verification:
   - Run `podman inspect --format '{{.State.Running}}' <name>`
   - If the container exists but is not running (crashed, stopped),
     remove and recreate it.
   - If `podman inspect` fails (container doesn't exist), create fresh.

2. In `LocalBackend.Launch()`, wrap the worker path in error handling:
   ```go
   handle, err := b.launchViaTaskWorker(ctx, spec, taskID)
   if err != nil {
       // Worker failed — fall back to ephemeral.
       logger.Sandbox.Warn("task worker failed, falling back to ephemeral",
           "task", taskID, "error", err)
       return b.launchEphemeral(ctx, spec)
   }
   ```

3. Add a periodic health check (optional, low priority): a background
   goroutine that checks worker liveness every 30 seconds and removes
   dead entries from the map.

## Tests

- `TestWorkerRecoveryAfterCrash` — kill the worker container externally,
  next exec auto-recovers.
- `TestFallbackToEphemeralOnWorkerFailure` — make worker creation fail
  (e.g., bad image), verify ephemeral fallback works.
- `TestHealthCheckRemovesDeadWorkers` — create a worker, kill it, run
  health check, verify cleaned up.

## Boundaries

- Do NOT change the `Backend` interface.
- The periodic health check is optional — the on-demand check in
  `ensureRunning()` is the primary mechanism.

## Implementation notes

All three items were already implemented in tasks 1 and 3:
- **Health check in `ensureRunning()`**: Implemented in task 1 — runs
  `podman inspect` to verify container is running, recreates on failure.
- **Graceful fallback in `Launch()`**: Implemented in task 3 — wraps
  worker path with error handling, falls back to ephemeral on failure.
- **Periodic health check**: Deferred as optional. The on-demand check
  in `ensureRunning()` is the primary mechanism and is sufficient.

Existing tests cover all three spec test cases:
- `TestTaskWorkerExecAfterStop` covers crash recovery
- `TestLaunchCreatesWorker` covers ephemeral fallback (worker fails, falls back)
- Periodic health check test deferred with the feature
