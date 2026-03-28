# Task 8: Watcher Multi-Store Task Iteration

**Status:** Done
**Depends on:** Task 1, Task 7
**Phase:** 3 (Handler Changes)
**Effort:** Large

## Goal

Watchers that scan for eligible tasks (promote, retry, test, submit,
refine) must check ALL active stores, not just the viewed one. Otherwise,
tasks in background groups are invisible to automation.

## What to do

1. Add a helper in `internal/handler/handler.go`:

   ```go
   // forEachActiveStore calls fn for every active workspace group's store.
   func (h *Handler) forEachActiveStore(fn func(s *store.Store, ws []string)) {
       for _, snap := range h.workspace.AllActiveSnapshots() {
           fn(snap.Store, snap.Workspaces)
       }
   }
   ```

2. Update `try*` methods in `tasks_autopilot.go` to iterate all stores:

   - `tryPromote()` — scan all stores for promotable backlog tasks;
     count in-progress across ALL stores for concurrency limit.
   - `tryRetry()` — scan all stores for retryable failed tasks.
   - `tryTest()` — scan all stores for testable tasks.
   - `trySubmit()` — scan all stores for submittable tasks.
   - `tryRefine()` — scan all stores for refineable tasks.
   - `trySyncWaiting()` — scan all stores for waiting tasks needing sync.

3. **Critical: concurrency limit across groups.** `CountRegularInProgress()`
   must sum across ALL active stores to enforce the global
   `WALLFACER_MAX_PARALLEL` limit:

   ```go
   func (h *Handler) countGlobalInProgress() int {
       total := 0
       h.forEachActiveStore(func(s *store.Store, _ []string) {
           total += s.CountRegularInProgress()
       })
       return total
   }
   ```

   Replace single-store `h.store.CountRegularInProgress()` calls with
   `h.countGlobalInProgress()`.

4. For state transitions (`UpdateTaskStatus`, `ResetTaskForRetry`, etc.)
   and event writes (`InsertEvent`), use the store that owns the task.
   Since `forEachActiveStore` iterates all stores, each task is found in
   its owning store naturally — no cross-store writes needed.

## Tests

- `TestTryPromoteAcrossGroups` — tasks in group A and B, verify both
  are considered for promotion.
- `TestConcurrencyLimitAcrossGroups` — 3 tasks in group A + 2 in group B,
  max parallel = 5, verify no more are promoted.
- `TestTryRetryAcrossGroups` — failed task in non-viewed group is retried.
- `TestWatcherDoesNotCrossStoreWrite` — verify state transitions happen
  in the task's owning store, not the viewed store.

## Boundaries

- Do NOT change API endpoint handlers (they correctly use viewed store).
- Do NOT change Runner.
