---
title: "Modify Switch() for Multi-Store Lifecycle"
status: complete
depends_on:
  - specs/foundations/multi-workspace-groups/task-01-active-groups-map.md
affects:
  - internal/workspace/manager.go
effort: medium
created: 2026-03-27
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 2: Modify Switch() for Multi-Store Lifecycle

## Goal

Change `Switch()` so it keeps old stores open when they have running tasks
instead of always closing them. This is the core lifecycle change that
enables concurrent workspace groups.

## What to do

1. Modify `Switch()` in `internal/workspace/manager.go`:

   - After creating the new snapshot, add it to `activeGroups`.
   - When cleaning up the old snapshot:
     - If `activeGroups[oldKey].taskCount > 0`, keep it in `activeGroups`
       (do NOT close the store).
     - If `taskCount == 0` AND `oldKey != newKey`, close the store and
       remove from `activeGroups`.
   - If switching back to a key already in `activeGroups`, reuse its
     existing store instead of creating a new one.

2. Store lifecycle rule: a store stays open when
   `taskCount > 0 OR key == m.current.Key`.

3. Update the cleanup section (currently after the atomic swap) to respect
   this rule.

## Tests

- `TestSwitchKeepsStoreForRunningTasks` — set `taskCount > 0` on group A,
  switch to B, verify A's store is NOT closed and remains in `activeGroups`.
- `TestSwitchClosesIdleGroup` — `taskCount == 0` on group A, switch to B,
  verify A's store IS closed and removed from `activeGroups`.
- `TestSwitchBackReusesStore` — switch A→B→A, verify A's store is reused
  (same pointer) if still in `activeGroups`.
- `TestSwitchToSameGroup` — switch to current group, verify no-op behavior
  preserved.

## Boundaries

- Do NOT change Runner or Handler.
- Do NOT remove the 409 check yet (that is task-06).
