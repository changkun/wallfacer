# Plan: Support Multiple Concurrent Workspace Groups

## Context

Wallfacer uses a **single active workspace group** model. The workspace
manager (`internal/workspace/manager.go`) holds one `Snapshot` at a time
containing a `Store`, workspace paths, instructions path, scoped data
directory, and a deterministic key. When the user switches groups:

- `Handler.UpdateWorkspaces()` (`workspace.go:76-88`) returns HTTP 409 if
  any task is `InProgress` or `Committing`.
- `Manager.Switch()` atomically swaps `m.current`, then **closes the
  previous store** (`lines 249-253`). The store's `Close()` sets an atomic
  `closed` flag but does not interrupt in-flight operations.
- The subscription goroutines in both Runner (`runner.go:622+`) and
  Handler (`handler.go:222-228`) call `applyWorkspaceSnapshot()` /
  `applySnapshot()`, which replace `r.store` / `h.store` with the new
  store reference.

This means switching groups stops all watchers from seeing the old group's
tasks (stale store reference) and prevents users from running tasks across
multiple workspace groups simultaneously.

**Goal**: Allow multiple workspace groups to have running tasks
simultaneously. Switching the "viewed" workspace group in the UI should not
stop tasks in other groups.

### Key Insight

Stores are already scoped by workspace key (`dataDir/<key>/`). Tasks
already use isolated worktrees. The only blocking issues are:

1. `Switch()` closes the old store immediately.
2. The 409 guard in `UpdateWorkspaces()` prevents switching entirely.
3. Watchers subscribe to a single store's `SubscribeWake()` at startup and
   never re-subscribe when the store changes.
4. `Runner.Run()` (`execute.go:134+`) reads `r.store` at entry and uses it
   throughout — if a workspace switch occurs mid-execution the reference
   becomes stale.

---

## Current Architecture (as of 2026-03-22)

### Manager

```go
// internal/workspace/manager.go
type Manager struct {
    mu        sync.RWMutex
    current   Snapshot            // single active group
    nextGen   uint64
    subsMu    sync.Mutex
    subs      map[int]chan Snapshot
    nextSubID int
    newStore  func(dir string) (*store.Store, error)
    // ...
}

type Snapshot struct {
    Workspaces       []string
    Store            *store.Store
    InstructionsPath string
    ScopedDataDir    string
    Key              string       // deterministic hash of sorted workspaces
    Generation       uint64
}
```

`Switch()` flow: validate → create new store at `dataDir/<key>` → atomic
swap under `mu.Lock` → publish to subscribers → close previous store.

### Runner

```go
// internal/runner/runner.go (lines 303-346)
type Runner struct {
    store            *store.Store        // swapped via applyWorkspaceSnapshot
    storeMu          sync.RWMutex        // guards store + workspace fields
    workspaces       []string            // workspace paths
    workspaceManager *workspace.Manager
    // ...
}
```

- `RunBackground(taskID, prompt, sessionID, resumedFromWaiting)` — spawns
  `Run()` in a goroutine tracked by `backgroundWg`.
- `Run()` reads `r.store` at entry (`execute.go:134+`) and uses it for
  all operations without re-checking.
- `startBoardSubscriptionLoop()` (`runner.go:622+`) subscribes to both
  workspace changes (via `wsMgr.Subscribe()`) and store changes (via
  `store.SubscribeWake()`). On workspace change it calls
  `applyWorkspaceSnapshot()` and re-subscribes to the new store.
- `RunBackground()` (`runner.go:482-489`) spawns `Run()` without workspace
  key capture or task count management.
- `currentStore()` (`runner.go:596-600`) returns `r.store` under `storeMu`
  read lock but is rarely used — most code reads `r.store` directly.

### Handler

```go
// internal/handler/handler.go (lines 98-173)
type Handler struct {
    snapshotMu sync.RWMutex
    store      *store.Store     // mirrors workspace.Manager.current.Store
    workspace  *workspace.Manager
    runner     runner.Interface
    workspaces []string
    // ...
}
```

- `currentStore()` reads from `h.workspace.Store()` (manager) directly.
- Six watchers (`tasks_autopilot.go`) each call `h.store.SubscribeWake()`
  at startup and reference `h.store` directly (~1000+ total references
  across the handler package).

| Watcher               | Start Line | Subscription |
|-----------------------|-----------|--------------|
| StartAutoPromoter     | 116       | `h.store.SubscribeWake()` |
| StartAutoRetrier      | 177       | `h.store.SubscribeWake()` |
| StartWaitingSyncWatcher| 504      | `h.store.SubscribeWake()` |
| StartAutoTester       | 662       | `h.store.SubscribeWake()` |
| StartAutoSubmitter    | 878       | `h.store.SubscribeWake()` |
| StartAutoRefiner      | 1079      | `h.store.SubscribeWake()` |

### Store

```go
// internal/store/store.go
type Store struct {
    dir     string
    closed  atomic.Bool
    tasks   map[uuid.UUID]*Task
    // wake subscribers: capacity-1 channels (coalescing)
    wakeSubscribers map[int]chan struct{}
    // ...
}
```

`Close()` sets `closed` flag atomically. Does NOT interrupt in-flight
reads/writes. `SubscribeWake()` returns a capacity-1 channel that coalesces
burst signals.

### Server Initialization (`server.go`)

```
wsMgr  = workspace.NewManager(...)
s      = wsMgr.Snapshot().Store
runner = runner.NewRunner(s, RunnerConfig{WorkspaceManager: wsMgr, ...})
handler= handler.NewHandler(s, runner, ...)
    → handler subscribes to wsMgr changes (goroutine: applySnapshot on each)
    → runner subscribes to wsMgr + store changes (startBoardSubscriptionLoop)
    → handler starts 6 watchers (each subscribes to s.SubscribeWake)
```

---

## Phase 1: Multi-Store Manager

**File**: `internal/workspace/manager.go` (~120 lines added)

### Changes

1. **Add `activeGroups` map** to Manager:

   ```go
   type activeGroup struct {
       snapshot  Snapshot
       taskCount atomic.Int32 // in-progress + committing tasks
   }

   // New fields in Manager (guarded by mu):
   activeGroups map[string]*activeGroup // key = Snapshot.Key
   ```

2. **Modify `Switch()`**:
   - Set `m.current` to the new snapshot (the "viewed" group).
   - Do NOT close the old store if `activeGroups[oldKey].taskCount > 0`.
     Keep it in `activeGroups`.
   - Always add the new snapshot to `activeGroups`.
   - Close and remove groups with `taskCount == 0` that are not the viewed
     group.

3. **New methods**:

   ```go
   // AllActiveSnapshots returns snapshots for all groups with open stores
   // (viewed group + groups with running tasks).
   func (m *Manager) AllActiveSnapshots() []Snapshot

   // StoreForKey returns the store for a workspace key, if still active.
   func (m *Manager) StoreForKey(key string) (*store.Store, bool)

   // IncrementTaskCount marks a new running task in the given group.
   // Called by Runner at task start.
   func (m *Manager) IncrementTaskCount(key string)

   // DecrementAndCleanup decrements the task count and closes the group's
   // store if it reaches zero and the group is not currently viewed.
   // Called by Runner at task completion.
   func (m *Manager) DecrementAndCleanup(key string)
   ```

4. **Store lifecycle rule**: A store stays open when
   `taskCount > 0 OR key == m.current.Key`.

### Tests

- `TestManagerMultiStoreLifecycle` — switch away from group A with running
  tasks; verify store stays open; decrement to zero; verify cleanup.
- `TestManagerSwitchBackReuseStore` — switch A→B→A; verify store A is
  reused (not re-created) if still in `activeGroups`.
- `TestManagerSingleGroupUnchanged` — single group, verify `activeGroups`
  has exactly one entry.

---

## Phase 2: Runner Multi-Store Awareness

**Files**: `runner.go` (~80 lines), `execute.go` (~30 lines),
`interface.go` (~5 lines), `mock.go` (~5 lines)

### Changes

1. **Capture store at task start** — `Run()` must resolve the correct store
   from the workspace manager at entry and use that reference throughout,
   rather than reading `r.store` which may have changed.

   ```go
   func (r *Runner) Run(taskID uuid.UUID, prompt, sessionID string, resumed bool) {
       // Resolve store for this task's workspace group.
       wsKey := r.taskWSKey.Load(taskID)
       s, ok := r.workspaceManager.StoreForKey(wsKey)
       if !ok {
           s = r.currentStore() // fallback to viewed store
       }
       // Use s (not r.store) for all operations in this execution.
       // ...
   }
   ```

   This is the most invasive change in the runner — `Run()` and the
   functions it calls (`executeTask`, `commitAndPush`, `runIdeationTask`,
   etc.) reference `r.store` extensively. Options:

   **Option A — per-execution context struct**: Wrap `s *store.Store` in a
   struct passed through the call chain. Clean but touches many signatures.

   **Option B — task-scoped store accessor**: Add a `taskStore(taskID)`
   method that checks `taskWSKey` then falls back to `currentStore()`.
   Replace `r.store` references with `r.taskStore(taskID)` calls. Less
   invasive but adds indirection.

   **Recommended**: Option B — smaller diff, same correctness.

2. **Track task-to-group mapping**:

   ```go
   // New fields in Runner:
   taskWSKey sync.Map // uuid.UUID → string (workspace key)
   ```

3. **`RunBackground` changes**:

   ```go
   func (r *Runner) RunBackground(taskID uuid.UUID, prompt, sessionID string, resumed bool) {
       // Capture current workspace key at dispatch time.
       wsKey := r.currentWSKey()
       r.taskWSKey.Store(taskID, wsKey)
       r.workspaceManager.IncrementTaskCount(wsKey)

       label := "run:" + taskID.String()[:8]
       r.backgroundWg.Add(label)
       go func() {
           defer r.backgroundWg.Done(label)
           defer r.taskWSKey.Delete(taskID)
           defer r.workspaceManager.DecrementAndCleanup(wsKey)
           r.Run(taskID, prompt, sessionID, resumed)
       }()
   }
   ```

4. **`applyWorkspaceSnapshot()`** — Continue updating `r.store` (the
   "viewed" store). In-flight tasks use their captured store via
   `taskStore()`.

5. **Board subscription loop** — No change needed. The loop already
   re-subscribes to the new store on workspace change. Board context only
   needs the viewed store.

---

## Phase 3: Handler and Watcher Changes

**Files**: `workspace.go` (~10 lines removed), `tasks_autopilot.go` (~120
lines refactored), `handler.go` (~30 lines), `config.go` (~10 lines)

### Changes

1. **Remove 409 blocking check** in `UpdateWorkspaces()`
   (`workspace.go:76-88`). This is the user-facing fix. Switches always
   succeed.

2. **Watcher store subscription** — The six watchers currently subscribe to
   `h.store.SubscribeWake()` at startup. When the viewed workspace changes,
   these subscriptions become stale (they still listen to the old store).

   **Fix**: Each watcher re-subscribes when the workspace snapshot changes.
   Add a helper:

   ```go
   // storeWakeChan returns a channel that fires on store changes,
   // automatically re-subscribing when the workspace group changes.
   // Caller must call cancel() on shutdown.
   func (h *Handler) storeWakeChan(ctx context.Context) (<-chan struct{}, func()) {
       // Subscribe to workspace changes.
       // On each change, unsubscribe old store wake, subscribe new.
       // Merge into single output channel.
   }
   ```

   Each `StartAuto*` method replaces its `h.store.SubscribeWake()` call
   with `h.storeWakeChan(ctx)`.

3. **Multi-store watcher iteration** — Watchers that scan for eligible
   tasks (promote, retry, test, submit, refine) must check ALL active
   stores, not just the viewed one. Add:

   ```go
   func (h *Handler) forEachActiveStore(fn func(s *store.Store, ws []string)) {
       for _, snap := range h.workspace.AllActiveSnapshots() {
           fn(snap.Store, snap.Workspaces)
       }
   }
   ```

   Replace direct `h.store.ListTasksByStatus(...)` calls in watcher
   `try*` methods with `h.forEachActiveStore(...)` iteration.

   **Scope**: The `try*` methods reference `h.store` or
   `h.currentStore()` in the following patterns:
   - `ListTasksByStatus` / `ListTasks` — task scanning
   - `CountRegularInProgress` — concurrency limit checks
   - `UpdateTaskStatus` / `ResetTaskForRetry` — state transitions
   - `InsertEvent` — event recording

   For state transitions and event writes, the correct store is the one
   that owns the task. Since `forEachActiveStore` iterates all stores, each
   task is found in its owning store naturally.

4. **`buildConfigResponse`** — Add `active_group_keys` field:

   ```go
   "active_group_keys": h.workspace.ActiveGroupKeys(),
   ```

5. **`currentStore()` / `currentWorkspaces()`** — No change. Continue
   returning the viewed group for API requests, git operations, SSE
   streams.

---

## Phase 4: Frontend

**Files**: `ui/js/workspace.js` (~20 lines), `ui/js/state.js` (~5 lines)

### Already Done

- `applyWorkspaceSelection()` (`workspace.js:856-880`) has no 409
  handling — the generic catch block shows errors but does not check
  status codes. No removal needed.

### Changes

1. **Activity indicator** — `renderWorkspaceGroups()` (lines 272-330)
   and `renderHeaderWorkspaceGroupTabs()` (lines 334-405): show a
   dot/badge next to groups that appear in `active_group_keys` from the
   config response.

2. **State** — Add `activeGroupKeys` to `state.js`, populated from
   config polling or SSE.

---

## Phase 5: Server Initialization

No fundamental changes. `server.go` already wires `wsMgr` into both
runner and handler. The `.env` file continues storing the viewed workspace
set for startup recovery. On startup only one store (the viewed group) is
open; `activeGroups` has one entry.

---

## Implementation Order

```
Phase 1 (Manager)    — foundation, no dependencies
Phase 2 (Runner)     — depends on Phase 1
Phase 3 (Handler)    — depends on Phase 1 & 2
Phase 4 (Frontend)   — depends on Phase 3
Phase 5 (Server)     — parallel with Phase 3, minimal changes
```

---

## Risk Areas

1. **Watcher refactoring scope** — The handler references `h.store`
   ~1000+ times across the package. Only the watcher `try*` methods need
   multi-store iteration (~6 methods, ~200 store references). Other
   handler methods (API endpoints) correctly use `currentStore()` for the
   viewed group. Careful scoping is needed to avoid changing API endpoint
   behavior.

2. **Runner store threading** — `Run()` and its callees (`executeTask`,
   `commitAndPush`, `runIdeationTask`, `runRefinement`, etc.) reference
   `r.store` extensively. The `taskStore(taskID)` accessor approach
   minimizes signature changes but every `r.store` reference in the
   execution path must be audited and replaced.

3. **Race between completion and cleanup** — After
   `DecrementAndCleanup`, before the store is actually closed, a new task
   could be created in that group (e.g. an idea-agent creating backlog
   tasks). The decrement and close must be atomic under `mu.Lock`, and
   task creation must check `activeGroups` before writing.

4. **Wake channel lifecycle** — When a store is closed, its wake
   subscribers receive no further signals. Watchers using `storeWakeChan`
   must handle the case where the channel becomes dead (the helper
   re-subscribes on workspace change, but a background group's store
   closing mid-scan needs graceful handling).

5. **Concurrency limit across groups** — `CountRegularInProgress()` is
   used to enforce the max parallel tasks limit. With multi-store, this
   count must span ALL active stores, not just the viewed one, to prevent
   exceeding the global limit.

---

## Verification

1. **Unit tests**: Manager multi-store lifecycle (open, reference count,
   cleanup, switch-back reuse).
2. **Unit tests**: Runner `taskStore()` resolution — correct store for
   in-flight task after workspace switch.
3. **Integration test**: Start tasks in group A, switch to group B,
   verify group A tasks continue running and reach `done`.
4. **Integration test**: Verify concurrency limit is enforced across
   groups.
5. **Manual test**: Open UI, create tasks in group A, switch to group B,
   create tasks there, verify both boards work independently.
6. **Regression**: Single-group usage unchanged (`activeGroups` has
   exactly one entry throughout).
7. Run existing test suite: `go test ./...`
