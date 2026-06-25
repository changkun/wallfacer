---
title: "Track Task-to-Group Mapping in Runner"
status: archived
depends_on:
  - specs/foundations/multi-workspace-groups/task-01-active-groups-map.md
affects:
  - internal/runner/runner.go
effort: small
created: 2026-03-27
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---


# Task 3: Track Task-to-Group Mapping in Runner

## Goal

Teach the Runner to remember which workspace group each task belongs to,
so in-flight tasks use the correct store even after a workspace switch.

## What to do

1. Add new field to Runner struct in `internal/runner/runner.go`:

   ```go
   taskWSKey sync.Map // uuid.UUID → string (workspace key)
   ```

2. Add a helper method:

   ```go
   // currentWSKey returns the key of the currently viewed workspace group.
   func (r *Runner) currentWSKey() string {
       r.storeMu.RLock()
       defer r.storeMu.RUnlock()
       return r.wsKey // add wsKey string field, set in applyWorkspaceSnapshot
   }
   ```

3. Update `applyWorkspaceSnapshot()` to also store the workspace key
   from the snapshot.

4. Add `taskStore(taskID)` accessor:

   ```go
   // taskStore returns the store for the workspace group that owns this task.
   // Falls back to currentStore() if the task's group is unknown or gone.
   func (r *Runner) taskStore(taskID uuid.UUID) *store.Store {
       if key, ok := r.taskWSKey.Load(taskID); ok {
           if s, ok := r.workspaceManager.StoreForKey(key.(string)); ok {
               return s
           }
       }
       return r.currentStore()
   }
   ```

## Tests

- `TestCurrentWSKey` — verify `currentWSKey()` returns the key from the
  latest applied snapshot.
- `TestTaskStoreResolution` — store a mapping, mock `StoreForKey`, verify
  `taskStore()` returns the correct store.
- `TestTaskStoreFallback` — no mapping stored, verify `taskStore()` falls
  back to `currentStore()`.

## Boundaries

- Do NOT change `RunBackground()` or `Run()` yet (tasks 4 and 5).
- Do NOT change Handler.
