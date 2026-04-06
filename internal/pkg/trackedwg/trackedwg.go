// Package trackedwg provides a sync.WaitGroup wrapper that tracks the labels
// of outstanding goroutines, useful for observability during graceful shutdown.
package trackedwg

import (
	"fmt"
	"slices"
	"sync"
)

// WaitGroup wraps sync.WaitGroup with per-label tracking so that callers can
// inspect which goroutines are still in flight. The zero value is ready for use.
type WaitGroup struct {
	mu      sync.Mutex
	pending map[string]int
	closed  bool // set by Wait; prevents Add after Wait to avoid sync.WaitGroup race
	wg      sync.WaitGroup
}

// Add increments the wait group counter and records label as pending.
// Add must be called before the goroutine that will call Done is started.
// Returns false if Wait has already been called (the add is silently dropped
// to avoid a sync.WaitGroup race between Add and Wait at counter zero).
func (w *WaitGroup) Add(label string) bool {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return false
	}
	if w.pending == nil {
		w.pending = make(map[string]int) // lazy init to support zero-value usage
	}
	w.pending[label]++
	// wg.Add inside the lock so it cannot race with Wait (which also acquires
	// the lock to set closed before calling wg.Wait).
	w.wg.Add(1)
	w.mu.Unlock()
	return true
}

// Done decrements the wait group counter and removes label from pending.
// The label must match a previous [Add] call. When the count for a label
// drops to zero, the entry is deleted to keep Pending output clean.
func (w *WaitGroup) Done(label string) {
	w.mu.Lock()
	if w.pending != nil {
		w.pending[label]--
		if w.pending[label] <= 0 {
			delete(w.pending, label)
		}
	}
	w.mu.Unlock()
	w.wg.Done()
}

// Go launches fn in a background goroutine tracked under label.
// It is shorthand for Add(label) followed by a goroutine that defers Done(label).
// If Wait has already been called, fn is not executed and Go returns false.
func (w *WaitGroup) Go(label string, fn func()) bool {
	if !w.Add(label) {
		return false
	}
	go func() {
		defer w.Done(label)
		fn()
	}()
	return true
}

// Wait blocks until all tracked goroutines have called Done. After Wait is
// called, subsequent Add calls are silently dropped.
func (w *WaitGroup) Wait() {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
	w.wg.Wait()
}

// Pending returns a sorted slice of labels for all goroutines that have not yet
// called Done. Labels with count > 1 are formatted as "label×N".
func (w *WaitGroup) Pending() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	result := make([]string, 0, len(w.pending))
	for label, count := range w.pending {
		if count == 1 {
			result = append(result, label)
		} else {
			result = append(result, fmt.Sprintf("%s×%d", label, count))
		}
	}
	slices.Sort(result)
	return result
}
