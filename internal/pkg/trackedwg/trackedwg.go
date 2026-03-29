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
	wg      sync.WaitGroup
}

// Add increments the wait group counter and records label as pending.
// Add must be called before the goroutine that will call Done is started.
func (w *WaitGroup) Add(label string) {
	w.mu.Lock()
	if w.pending == nil {
		w.pending = make(map[string]int) // lazy init to support zero-value usage
	}
	w.pending[label]++
	w.mu.Unlock()
	// wg.Add must happen after recording the label so that Pending is always
	// consistent: if a caller checks Pending after Add returns, the label is
	// guaranteed to be visible.
	w.wg.Add(1)
}

// Done decrements the wait group counter and removes label from pending.
// The label must match a previous [Add] call. When the count for a label
// drops to zero, the entry is deleted to keep Pending output clean.
func (w *WaitGroup) Done(label string) {
	w.mu.Lock()
	w.pending[label]--
	if w.pending[label] <= 0 {
		delete(w.pending, label)
	}
	w.mu.Unlock()
	w.wg.Done()
}

// Go launches fn in a background goroutine tracked under label.
// It is shorthand for Add(label) followed by a goroutine that defers Done(label).
func (w *WaitGroup) Go(label string, fn func()) {
	w.Add(label)
	go func() {
		defer w.Done(label)
		fn()
	}()
}

// Wait blocks until all tracked goroutines have called Done.
func (w *WaitGroup) Wait() {
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
