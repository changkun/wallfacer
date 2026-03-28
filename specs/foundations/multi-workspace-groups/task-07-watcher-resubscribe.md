# Task 7: Watcher Store Re-subscription on Workspace Change

**Status:** Done
**Depends on:** Task 1
**Phase:** 3 (Handler Changes)
**Effort:** Medium

## Goal

Fix the stale wake channel problem: watchers currently subscribe to
`h.store.SubscribeWake()` once at startup. When the viewed workspace
changes, these channels go dead. Add a helper that automatically
re-subscribes watchers to the new store.

## What to do

1. Add a helper in `internal/handler/handler.go` (or a new file
   `internal/handler/watcher_wake.go`):

   ```go
   // storeWakeChan returns a merged channel that fires on store changes,
   // automatically re-subscribing when the workspace group changes.
   func (h *Handler) storeWakeChan(ctx context.Context) (<-chan struct{}, func()) {
       // 1. Subscribe to workspace changes via h.workspace.Subscribe()
       // 2. Subscribe to current store's SubscribeWake()
       // 3. On workspace change: unsubscribe from old store, subscribe to new
       // 4. Forward all wake signals to a single output channel
       // 5. Cancel func cleans up both subscriptions
   }
   ```

2. Update each `StartAuto*` method in `tasks_autopilot.go` to use
   `h.storeWakeChan(ctx)` instead of `Wake: h.store`:

   | Watcher               | Current              | New                         |
   |-----------------------|---------------------|-----------------------------|
   | StartAutoPromoter     | `Wake: h.store`     | `h.storeWakeChan(ctx)`      |
   | StartAutoRetrier      | `Wake: h.store`     | `h.storeWakeChan(ctx)`      |
   | StartAutoTester       | `Wake: h.store`     | `h.storeWakeChan(ctx)`      |
   | StartAutoSubmitter    | `Wake: h.store`     | `h.storeWakeChan(ctx)`      |
   | StartAutoRefiner      | `Wake: h.store`     | `h.storeWakeChan(ctx)`      |

3. Handle the edge case where a background group's store is closed
   (task count drops to zero) — the wake channel should stop forwarding
   without crashing.

## Tests

- `TestStoreWakeChanResubscribes` — trigger workspace change, verify wake
  signals from the new store are forwarded.
- `TestStoreWakeChanOldStoreClosed` — close the old store, verify no panic
  or deadlock.
- `TestStoreWakeChanCancelCleanup` — call cancel, verify subscriptions
  are cleaned up.

## Boundaries

- Do NOT change watcher iteration logic (task 8).
- Keep `h.store` field and `applySnapshot()` unchanged.
