---
title: Multiple Concurrent Workspace Groups
status: archived
depends_on: []
affects:
  - internal/workspace/
  - internal/runner/
  - internal/handler/
effort: xlarge
created: 2026-03-27
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---


# Multiple Concurrent Workspace Groups

## Problem

The workspace manager held a single active `Snapshot` at a time. Switching groups had two consequences:

1. **Blocking switches.** `UpdateWorkspaces()` returned HTTP 409 if any task was `InProgress` or `Committing`. Users had to wait for all tasks to finish before context-switching.

2. **Store closure killed background work.** `Switch()` closed the previous store immediately. Watchers lost their subscriptions, in-flight `Run()` calls held stale store references, and all automation for the old group stopped.

Stores were already scoped by workspace key (`dataDir/<key>/`) and tasks already used isolated worktrees. The blocking issues were purely in lifecycle management, not data isolation.

## Strategy

Keep stores alive via reference counting rather than introducing a new storage abstraction. Each task captures its workspace key at dispatch and resolves its own store throughout execution, independent of which group the user is viewing. Automation watchers iterate all active stores, enforcing concurrency limits globally. The 409 guard was removed — switches always succeed instantly.

The work was done in four phases: manager lifecycle, runner store binding, handler/watcher refactoring, and frontend badges. Each phase was independently testable.

## Design

### Store Lifecycle

A store stays open when `taskCount > 0 OR key == m.current.Key`. The manager tracks an `activeGroups` map keyed by workspace key. Each entry holds a snapshot and an atomic task count.

- `Switch()` keeps the previous group's store alive when tasks are running and cleans it up when the count reaches zero.
- Switching back to a group reuses the existing store rather than creating a new one.
- `DecrementAndCleanup()` atomically decrements the count and closes the store under the write lock, preventing a race where a new task could be created in the group between decrement and close.

### Key Decisions

- **Task-scoped store accessor over context struct.** `taskStore(taskID)` checks the task-to-group mapping then falls back to `currentStore()`. This replaced ~50 direct `r.store` references without touching function signatures across the runner call chain.
- **`resubscribingWakeSource` for watchers.** A wrapper that monitors workspace changes and re-subscribes to the new store's wake channel automatically. Keeps watcher code unchanged; subscription management is encapsulated in `watcher_wake.go`.
- **Global concurrency counting.** `countGlobalInProgress()` and `countGlobalTestsInProgress()` aggregate across all active stores. This prevents exceeding `WALLFACER_MAX_PARALLEL` when tasks span multiple groups.
- **Reference counting over GC.** Deterministic cleanup — no background sweep or finalizer needed. The store closes synchronously when the last task completes and the group is not viewed.
- **Config API exposes `active_groups` with per-group task counts.** Richer than just keys — includes `in_progress` and `waiting` counts so the frontend can render meaningful badges without additional API calls.

## Outcome

### Manager (`internal/workspace/manager.go`)

New fields: `activeGroups map[string]*activeGroup` with `taskCount atomic.Int32`. New methods: `AllActiveSnapshots()`, `StoreForKey()`, `IncrementTaskCount()`, `DecrementAndCleanup()`, `ActiveGroupKeys()`.

### Runner (`internal/runner/`)

`RunBackground()` captures the workspace key at dispatch, increments the group's task count, and defers cleanup. `taskStore(taskID)` resolves the correct store for each in-flight task. `taskWSKey sync.Map` tracks the task-to-group mapping.

### Handler (`internal/handler/`)

- 409 blocking check in `UpdateWorkspaces()` removed.
- `resubscribingWakeSource` (`watcher_wake.go`) used by all five `StartAuto*` methods.
- `forEachActiveStore()` lets all six `try*` watcher methods scan tasks across every active group.
- `buildConfigResponse()` includes `active_groups` with per-group task counts.

### Frontend (`ui/js/`)

Workspace group tabs and the settings panel show live badges: spinning icon with count for in-progress tasks, pause icon for waiting tasks. The viewed group uses SSE-synced counts for immediate updates; background groups use the config polling response.

### Server Initialization

No changes. On startup one store opens for the restored workspace group; `activeGroups` starts with a single entry and grows as tasks run across groups.

## Design Evolution

The spec originated as a detailed five-phase implementation plan with code blocks showing proposed struct layouts, method signatures, and watcher refactoring patterns. Several aspects evolved during implementation:

1. **`active_group_keys` became `active_groups`.** The original spec proposed exposing just the keys. The implementation added per-group `in_progress` and `waiting` counts, making the config response self-contained for badge rendering.

2. **`storeWakeChan` became `resubscribingWakeSource`.** The spec proposed a function returning a channel and cancel func. The implementation used a struct with a background goroutine for cleaner lifecycle management.

3. **Concurrency limit enforcement was explicit.** The spec noted the risk but didn't prescribe a solution. The implementation added `countGlobalInProgress()` and `countGlobalTestsInProgress()` as dedicated helpers.

## Future Work

None planned. The feature is complete. Remote backends (see `specs/cloud/`) will need to ensure their store lifecycle integrates with `activeGroups` when implemented.
