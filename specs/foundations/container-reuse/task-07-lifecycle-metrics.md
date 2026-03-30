---
title: "Worker Lifecycle Timing Metrics"
status: complete
depends_on:
  - specs/foundations/container-reuse/task-03-launch-routing.md
affects:
  - internal/sandbox/worker.go
  - internal/metrics/
effort: small
created: 2026-03-27
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 7: Worker Lifecycle Timing Metrics

## Goal

Add span events and metrics for worker lifecycle operations so the
performance impact of container reuse can be measured.

## What to do

1. In `taskWorker.ensureRunning()`, emit timing spans for:
   - `container_create` — time for `podman create` + `podman start`
   - `container_health_check` — time for `podman inspect`

2. In `taskWorker.exec()`, emit a span for:
   - `container_exec` — time from exec start to process launch

3. In `LocalBackend.Launch()`, record whether the worker path or
   ephemeral path was taken (for dashboarding):
   - Add a label to the container spec or a metric counter

4. Add Prometheus-compatible counters via `metrics.Registry` (if
   available):
   - `wallfacer_container_worker_creates_total`
   - `wallfacer_container_worker_execs_total`
   - `wallfacer_container_worker_fallbacks_total` (ephemeral fallback)

## Tests

- `TestWorkerMetricsRecorded` — verify counters increment on create,
  exec, and fallback.

## Boundaries

- Do NOT change the agent output format.
- Metrics are optional (no-op when registry is nil).
