package handler

import (
	"changkun.de/x/wallfacer/internal/store"
	"changkun.de/x/wallfacer/internal/workspace"
)

// resubscribingWakeSource implements watcher.WakeSource by forwarding wake
// signals from the currently viewed store, automatically re-subscribing
// when the workspace group changes. Without this adapter, autopilot watchers
// would stop receiving task-change notifications after a workspace switch
// because the underlying store (and its pub/sub hub) is replaced.
type resubscribingWakeSource struct {
	wakeCh chan struct{} // capacity-1 output channel forwarded to the watcher
	done   chan struct{} // closed by UnsubscribeWake to stop the goroutine
}

// newResubscribingWakeSource creates a WakeSource that tracks workspace
// changes and re-subscribes to the new store's wake channel on each switch.
// The returned source must be used as the Wake field in a watcher.Config.
// The watcher's deferred UnsubscribeWake call will stop the background goroutine.
func (h *Handler) newResubscribingWakeSource() *resubscribingWakeSource {
	s := &resubscribingWakeSource{
		wakeCh: make(chan struct{}, 1),
		done:   make(chan struct{}),
	}

	// Subscribe to workspace changes from the manager.
	var wsSubID int
	var wsCh <-chan workspace.Snapshot
	if h.workspace != nil {
		wsSubID, wsCh = h.workspace.Subscribe()
	}

	// Subscribe to the current store's wake channel.
	var storeWakeID int
	var storeWakeCh <-chan struct{}
	h.snapshotMu.RLock()
	currentStore := h.store
	h.snapshotMu.RUnlock()
	if currentStore != nil {
		storeWakeID, storeWakeCh = currentStore.SubscribeWake()
	}

	go s.run(h, currentStore, storeWakeID, storeWakeCh, wsSubID, wsCh)
	return s
}

// run is the forwarding loop. It forwards store wake signals to s.wakeCh
// and re-subscribes to the new store on workspace changes.
func (s *resubscribingWakeSource) run(
	h *Handler,
	currentStore *store.Store,
	storeWakeID int,
	storeWakeCh <-chan struct{},
	wsSubID int,
	wsCh <-chan workspace.Snapshot,
) {
	defer func() {
		// Unsubscribe from the current store wake.
		if currentStore != nil {
			currentStore.UnsubscribeWake(storeWakeID)
		}
		// Unsubscribe from workspace changes.
		if h.workspace != nil {
			h.workspace.Unsubscribe(wsSubID)
		}
	}()

	for {
		select {
		case <-s.done:
			return

		case <-storeWakeCh:
			// Forward wake signal to the output channel (coalescing).
			select {
			case s.wakeCh <- struct{}{}:
			default:
			}

		case snap, ok := <-wsCh:
			if !ok {
				return // workspace manager shut down
			}
			// Unsubscribe from the old store.
			if currentStore != nil {
				currentStore.UnsubscribeWake(storeWakeID)
			}
			// Subscribe to the new store.
			currentStore = snap.Store
			if currentStore != nil {
				storeWakeID, storeWakeCh = currentStore.SubscribeWake()
			} else {
				storeWakeCh = nil
			}
			// Signal the watcher so it re-scans with the new store.
			select {
			case s.wakeCh <- struct{}{}:
			default:
			}
		}
	}
}

// SubscribeWake implements watcher.WakeSource. It returns the shared output
// channel. Only one consumer (the watcher) should call this.
func (s *resubscribingWakeSource) SubscribeWake() (int, <-chan struct{}) {
	return 0, s.wakeCh
}

// UnsubscribeWake implements watcher.WakeSource. It stops the background
// goroutine and cleans up subscriptions. Safe to call multiple times.
func (s *resubscribingWakeSource) UnsubscribeWake(_ int) {
	select {
	case <-s.done:
		// Already closed.
	default:
		close(s.done)
	}
}
