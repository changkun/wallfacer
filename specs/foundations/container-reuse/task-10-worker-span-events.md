# Task 10: Worker Lifecycle Stats in Settings Panel

**Status:** Done (fully implemented)
**Depends on:** Task 7
**Phase:** 3 (Robustness)
**Effort:** Small

## Goal

Show aggregate worker lifecycle statistics in the system settings or
debug panel so users can see the impact of container reuse — how many
workers were created, how many execs reused them, and how many fell
back to ephemeral.

## Context

Task 7 added Prometheus counters (`wallfacer_container_worker_creates_total`,
`wallfacer_container_worker_execs_total`, `wallfacer_container_worker_fallbacks_total`).
These are available via `/api/debug/runtime` or a metrics scrape endpoint.
This task makes them visible in the frontend.

## What to do

1. **Expose worker stats in the runtime debug endpoint**
   (`GET /api/debug/runtime`). In `internal/handler/debug.go`, add a
   `worker_stats` section to the response:

   ```json
   {
     "worker_stats": {
       "creates": 12,
       "execs": 45,
       "fallbacks": 2,
       "active_workers": 3,
       "enabled": true
     }
   }
   ```

   Read counter values from the metrics registry. Read `active_workers`
   count and `enabled` flag from the backend (add a `WorkerStats()`
   method to the `WorkerManager` interface or `LocalBackend`).

2. **Display in the frontend** — Add a "Container Workers" section to
   the existing runtime debug modal (`/api/debug/runtime` data is already
   shown somewhere in settings or debug views). Show:

   - Enabled: yes/no
   - Active workers: N
   - Total creates / execs / fallbacks
   - Reuse ratio: `execs / (execs + fallbacks)` as a percentage

## Tests

- `TestDebugRuntimeIncludesWorkerStats` — verify the runtime endpoint
  includes the `worker_stats` field.

## Boundaries

- Do NOT add per-task span events (the aggregate view is sufficient).
- Do NOT change the metrics endpoint format.

## Implementation notes

- **Backend (item 1)**: `worker_stats` added to `GET /api/debug/runtime`
  with `enabled` and `active_workers` fields. `WorkerStats()` method added
  to `WorkerManager` interface, implemented by `LocalBackend`.
- **Frontend (item 2)**: System Status section added to Settings > About
  tab. Fetches runtime data on tab open and shows goroutines, heap,
  active containers, circuit breaker state, task worker status, and task
  counts. Implemented in `loadSystemStatus()` in `status-bar.js`.
