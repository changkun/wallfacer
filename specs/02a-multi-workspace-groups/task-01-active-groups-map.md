# Task 1: Add activeGroups Map to Manager

**Status:** Done
**Depends on:** None
**Phase:** 1 (Multi-Store Manager)
**Effort:** Small

## Goal

Add the `activeGroups` data structure to the workspace Manager so it can
track multiple open stores simultaneously. This is the foundation for all
subsequent tasks.

## What to do

1. Add new types and fields in `internal/workspace/manager.go`:

   ```go
   type activeGroup struct {
       snapshot  Snapshot
       taskCount atomic.Int32 // in-progress + committing tasks
   }
   ```

   Add to Manager struct (guarded by existing `mu`):

   ```go
   activeGroups map[string]*activeGroup // key = Snapshot.Key
   ```

2. Initialize `activeGroups` in `NewManager()` with the initial snapshot
   as the sole entry.

3. Add accessor methods:

   ```go
   // AllActiveSnapshots returns snapshots for all groups with open stores.
   func (m *Manager) AllActiveSnapshots() []Snapshot

   // StoreForKey returns the store for a workspace key, if still active.
   func (m *Manager) StoreForKey(key string) (*store.Store, bool)

   // ActiveGroupKeys returns keys for all groups with open stores.
   func (m *Manager) ActiveGroupKeys() []string
   ```

   All three acquire `mu.RLock`.

4. Add task-count management methods:

   ```go
   // IncrementTaskCount marks a new running task in the given group.
   func (m *Manager) IncrementTaskCount(key string)

   // DecrementAndCleanup decrements the task count. If it reaches zero
   // and the group is not currently viewed, close and remove the group.
   func (m *Manager) DecrementAndCleanup(key string)
   ```

   `DecrementAndCleanup` must acquire `mu.Lock` to atomically check
   the count AND remove the entry, preventing a race where a new task
   is created between decrement and cleanup.

## Tests

- `TestActiveGroupsInitialization` — after `NewManager`, `activeGroups`
  has exactly one entry matching the initial snapshot.
- `TestIncrementDecrementTaskCount` — increment to N, decrement to 0
  on a non-viewed group, verify group is cleaned up.
- `TestDecrementViewedGroupNotRemoved` — decrement to 0 on the viewed
  group, verify it stays in `activeGroups`.
- `TestAllActiveSnapshots` — with 2+ groups, returns all.
- `TestStoreForKey` — returns correct store; returns false for unknown key.

## Boundaries

- Do NOT modify `Switch()` yet (that is task-02).
- Do NOT change Runner or Handler.
