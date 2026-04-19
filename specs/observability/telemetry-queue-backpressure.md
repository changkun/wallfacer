---
title: Telemetry Queue Backpressure
status: drafted
depends_on:
  - specs/identity/data-boundary-enforcement.md
affects:
  - internal/cloud/
  - internal/store/
effort: small
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Telemetry Queue Backpressure

## Problem

Wallfacer local instances queue telemetry events when the cloud is unreachable, then retry with exponential backoff. If the cloud is down for hours or days, the local queue grows unbounded. This consumes disk space, slows down the local UI, and eventually fails silently when disk fills.

We need a cap on queue size and a clear policy for what happens when it's reached.

## Scope

Define and enforce a maximum local queue size for telemetry events. When the cap is hit, drop the oldest events (preserving the newest, which are most actionable) and log loudly.

## Design

### Queue storage

Events are persisted to `data/telemetry-queue.jsonl` using the existing `internal/store/` atomic-write pattern. On server start, load the queue, begin background retry loop.

### Cap and policy

- Maximum queue size: **10,000 events** (configurable via `WALLFACER_TELEMETRY_QUEUE_CAP` env var)
- When cap is reached:
  1. Drop the oldest event
  2. Append the new event
  3. Increment a `telemetry_events_dropped_total` Prometheus counter
  4. Log once per hour at WARN level: "telemetry queue full, dropping oldest events; N events dropped total"
- When queue drops below 50% capacity: log once at INFO level "telemetry queue recovered; N events pending"

### Retry loop

- Background goroutine attempts to flush queue to cloud every 30 seconds
- On success: remove flushed events from queue
- On failure: exponential backoff, max 5 minutes between retries
- Never block task execution on cloud telemetry — telemetry is strictly additive

### UI indicator

If queue has >1000 events pending (i.e., cloud is extended-down), show a subtle banner in wallfacer UI: "Cloud sync paused — N events queued, will sync when connectivity returns." This is honesty, not an error.

## Testing

- Unit: inject 10,001 events, verify oldest is dropped
- Unit: verify counter increments on drop
- Integration: simulate 48h cloud outage, verify local wallfacer remains fully functional and queue caps at 10K
- Integration: verify recovery — restart cloud, queue drains, banner clears

## Implementation

- `internal/cloud/queue.go` — queue with cap and drop-oldest policy
- `internal/cloud/queue_test.go` — tests
- `internal/metrics/` — add `telemetry_events_dropped_total` counter
- UI: add queue-status banner to task board header

## Success

- Long cloud outages (48+ hours) do not break local wallfacer
- Disk usage from telemetry queue is bounded at ~5MB max (10K events × ~500 bytes)
- Metrics show drop rate, making extended outages visible operationally
