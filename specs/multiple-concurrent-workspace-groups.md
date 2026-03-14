# Plan: Support Multiple Concurrent Workspace Groups

## Context

Currently, wallfacer has a **single active workspace group** model. The workspace manager holds one `Snapshot` (store + workspaces + instructions) at a time. When switching workspace groups, `UpdateWorkspaces` returns HTTP 409 if any tasks are `InProgress` or `Committing`. The old store is **closed** on switch. This prevents users from running tasks across multiple workspace groups simultaneously.

**Goal**: Allow multiple workspace groups to have running tasks simultaneously. Switching the "viewed" workspace group in the UI should not stop tasks in other groups.

## Approach: Hybrid Multi-Store

The Manager holds multiple open stores (one per group with active tasks), while the Handler and Runner maintain a "viewed" pointer for the UI and API. This minimizes changes while achieving full concurrency.

### Key Insight

Stores are already scoped by workspace-key (`dataDir/<key>/`). Tasks already use isolated worktrees. The only blocking issue is that `Switch()` closes the old store and the watchers/handlers lose reference to it.

---

## Phase 1: Multi-Store Manager (`internal/workspace/manager.go`)

### Changes

1. **Add `activeStores` map** to Manager:
   ```go
   type activeGroup struct {
       snapshot  Snapshot
       taskCount int32 // atomic: in-progress + committing tasks
   }

   // New fields in Manager:
   activeStores map[string]*activeGroup // key = workspace key
   ```

2. **Modify `Switch()`**:
   - Still set `m.current` to the new snapshot (the "viewed" group)
   - Do NOT close the old store if it has `taskCount > 0`; keep it in `activeStores`
   - Always add the new store to `activeStores`
   - Close and remove stores with `taskCount == 0` that are NOT the viewed group

3. **New methods**:
   - `AllActiveStores() []Snapshot` — returns snapshots for all groups with open stores
   - `StoreForKey(key string) (*store.Store, bool)` — lookup by workspace key
   - `IncrementTaskCount(key string)` / `DecrementTaskCount(key string)` — track running tasks
   - `CleanupIdleStores()` — close non-viewed stores with zero running tasks

4. **Store lifecycle rule**: A store stays open if `taskCount > 0` OR it is the viewed group.

### Files
- `internal/workspace/manager.go` (~100 lines added)
- `internal/workspace/manager_test.go` (new tests)

---

## Phase 2: Runner Multi-Store Awareness (`internal/runner/`)

### Changes

1. **Store registry** — Replace single `r.store` with:
   ```go
   storeRegistry map[string]*store.Store // key = workspace key
   viewedKey     string
   taskStoreKey  sync.Map // task UUID -> workspace key
   ```

2. **Task-store mapping** — When `RunBackground` is called, record which workspace key the task belongs to. Resolve the correct store in `Run()` via this mapping.

3. **`RunBackground` signature change**:
   - Add `wsKey string` parameter (the workspace key for the task's store)
   - Call `manager.IncrementTaskCount(wsKey)` at start
   - Call `manager.DecrementTaskCount(wsKey)` + `manager.CleanupIdleStores()` on completion

4. **`applyWorkspaceSnapshot()`** — Add store to `storeRegistry` instead of replacing. Update `viewedKey`.

5. **Board subscription loop** — Subscribe to wake events from ALL stores in registry, not just the viewed one.

### Files
- `internal/runner/runner.go` (~80 lines modified)
- `internal/runner/execute.go` (~20 lines — store resolution at Run entry)
- `internal/runner/interface.go` (~5 lines — RunBackground signature)
- `internal/runner/mock.go` (update mock)

---

## Phase 3: Handler Changes (`internal/handler/`)

### Changes

1. **Remove the 409 blocking check** in `UpdateWorkspaces()` (`workspace.go:74-86`). This is the user-facing fix.

2. **Auto-watchers** — The watchers (`StartAutoPromoter`, `StartAutoRetrier`, `StartAutoTester`, `StartAutoSubmitter`, `StartAutoRefiner`, `StartIdeationWatcher`) currently subscribe to `h.store.SubscribeWake()` at startup. This is already broken across workspace switches (they stay subscribed to the initial store).

   **Fix**: Replace direct store subscription with workspace-manager-aware subscription:
   - Add `h.forEachActiveStore(fn func(s *store.Store, ws []string))` helper
   - Each watcher's `try*` method iterates all active stores instead of using `h.store` directly
   - Use the ticker-based fallback polling (already present as 60s ticker in `StartAutoPromoter`) instead of trying to merge wake channels from multiple stores
   - Alternative: subscribe to workspace manager for new-store events and dynamically add wake subscriptions

   **Pragmatic approach**: Since watchers already have ticker fallbacks (60s), simplify to:
   - Watchers subscribe to workspace manager for view changes
   - On each tick or wake, iterate `manager.AllActiveStores()` and run checks against each

3. **`tryAutoPromote` and similar** — Change from `h.store.ListTasksByStatus(...)` to iterating all active stores.

4. **`buildConfigResponse`** — Add `active_group_keys` field listing workspace keys with running tasks.

5. **`currentStore()` / `currentWorkspaces()`** — Continue returning the viewed group (for API requests, git operations, etc.).

### Files
- `internal/handler/workspace.go` (~10 lines removed)
- `internal/handler/tasks_autopilot.go` (~100 lines refactored)
- `internal/handler/ideate.go` (~20 lines)
- `internal/handler/config.go` (~15 lines)
- `internal/handler/handler.go` (~20 lines — forEachActiveStore helper)

---

## Phase 4: Frontend (`ui/js/`)

### Changes

1. **`workspace.js`** — `applyWorkspaceSelection()`:
   - Remove expectation of 409 errors (switch always succeeds now)
   - Keep stream restart logic (correct — board should show new group)

2. **`workspace.js`** — `renderWorkspaceGroups()` / `renderHeaderWorkspaceGroupsMenu()`:
   - Show activity indicator (spinner/badge) next to groups with running tasks
   - Info comes from new `active_group_keys` in config response

3. **`state.js`** — Add `backgroundGroupActivity` state variable.

### Files
- `ui/js/workspace.js` (~20 lines)
- `ui/js/state.js` (~5 lines)

---

## Phase 5: Server Initialization (`server.go`)

Minor adjustments to pass workspace manager references correctly. The `.env` file continues storing the viewed workspace set for startup recovery. No fundamental changes.

---

## Implementation Order

```
Phase 1 (Manager)    — foundation, no dependencies
Phase 2 (Runner)     — depends on Phase 1
Phase 3 (Handler)    — depends on Phase 1 & 2
Phase 4 (Frontend)   — depends on Phase 3
Phase 5 (Server)     — parallel with Phase 3
```

---

## Risk Areas

1. **Auto-watcher multi-store iteration** — Most complex change. The watchers use `h.store` directly ~30 times across `tasks_autopilot.go`. Each needs refactoring to iterate all active stores.

2. **Store reference in `Run()`** — The `Run` method references `r.store` extensively. Need to resolve the correct store at entry and thread it through. A per-execution context struct would be cleanest.

3. **Race between task completion and store cleanup** — After `DecrementTaskCount`, before `CleanupIdleStores`, another task could start. Must use proper locking in the Manager.

4. **Wake subscription management** — Dynamically adding/removing store subscriptions as groups become active/idle requires careful lifecycle management.

---

## Verification

1. **Unit tests**: Manager multi-store lifecycle (open, reference count, cleanup)
2. **Integration test**: Start tasks in group A, switch to group B, verify group A tasks continue running
3. **Manual test**: Open UI, create tasks in group A, switch to group B, create tasks there, verify both boards work independently
4. **Regression**: Verify single-group usage is unchanged (activeStores has exactly one entry)
5. Run existing test suite: `go test ./...`
