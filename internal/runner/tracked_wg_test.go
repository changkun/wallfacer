package runner

import (
	"sync"
	"testing"
	"time"
)

// TestTrackedWgSequential verifies that a single Add/Done cycle correctly
// updates Pending before and after the Done call.
func TestTrackedWgSequential(t *testing.T) {
	var wg trackedWg

	wg.Add("alpha")

	pending := wg.Pending()
	if len(pending) != 1 || pending[0] != "alpha" {
		t.Fatalf("expected [alpha] after Add, got %v", pending)
	}

	wg.Done("alpha")
	wg.Wait()

	pending = wg.Pending()
	if len(pending) != 0 {
		t.Fatalf("expected empty Pending after Done, got %v", pending)
	}
}

// TestTrackedWgMultipleDistinctLabels verifies that distinct labels are tracked
// independently and that Pending returns them in sorted order.
func TestTrackedWgMultipleDistinctLabels(t *testing.T) {
	var wg trackedWg

	wg.Add("charlie")
	wg.Add("alpha")
	wg.Add("bravo")

	pending := wg.Pending()
	if len(pending) != 3 {
		t.Fatalf("expected 3 pending labels, got %v", pending)
	}
	want := []string{"alpha", "bravo", "charlie"}
	for i, label := range want {
		if pending[i] != label {
			t.Errorf("pending[%d]: want %q, got %q", i, label, pending[i])
		}
	}

	wg.Done("alpha")
	wg.Done("bravo")
	wg.Done("charlie")
	wg.Wait()

	if p := wg.Pending(); len(p) != 0 {
		t.Fatalf("expected empty Pending after all Done calls, got %v", p)
	}
}

// TestTrackedWgRepeatedLabel verifies that duplicate labels are reported as
// "label×N" when count > 1, and then correctly decremented on each Done.
func TestTrackedWgRepeatedLabel(t *testing.T) {
	var wg trackedWg

	wg.Add("foo")
	wg.Add("foo")

	pending := wg.Pending()
	if len(pending) != 1 || pending[0] != "foo×2" {
		t.Fatalf("expected [foo×2] after two Add calls, got %v", pending)
	}

	wg.Done("foo")
	pending = wg.Pending()
	if len(pending) != 1 || pending[0] != "foo" {
		t.Fatalf("expected [foo] after first Done, got %v", pending)
	}

	wg.Done("foo")
	wg.Wait()

	if p := wg.Pending(); len(p) != 0 {
		t.Fatalf("expected empty Pending after second Done, got %v", p)
	}
}

// TestTrackedWgWaitBlocks verifies that Wait blocks until Done is called.
func TestTrackedWgWaitBlocks(t *testing.T) {
	var wg trackedWg
	const delay = 20 * time.Millisecond

	wg.Add("work")
	go func() {
		time.Sleep(delay)
		wg.Done("work")
	}()

	start := time.Now()
	wg.Wait()
	elapsed := time.Since(start)

	if elapsed < delay {
		t.Errorf("Wait returned too early: elapsed %v, want >= %v", elapsed, delay)
	}
}

// TestTrackedWgConcurrentAddDone launches many goroutines all using the same
// label concurrently. Running with -race should detect any missing mutex
// protection. After Wait returns, Pending must be empty.
func TestTrackedWgConcurrentAddDone(t *testing.T) {
	var wg trackedWg
	const n = 50

	var start sync.WaitGroup
	start.Add(n)

	for i := 0; i < n; i++ {
		go func() {
			wg.Add("task")
			start.Done()
			// simulate a small amount of work
			time.Sleep(time.Millisecond)
			wg.Done("task")
		}()
	}

	// Wait for all goroutines to have called Add before calling Wait, so the
	// WaitGroup counter is guaranteed to be positive when Wait is invoked.
	start.Wait()
	wg.Wait()

	if p := wg.Pending(); len(p) != 0 {
		t.Fatalf("expected empty Pending after concurrent Wait, got %v", p)
	}
}

// TestTrackedWgAddOrdering documents and verifies the invariant that Add must
// be called *before* the goroutine that will call Done is started.
//
// The implementation (runner.go:26-34) increments the pending map under the
// mutex and then calls wg.Add(1) *after* releasing the mutex. This means that
// if a goroutine called Done before Add's wg.Add(1) executes, the underlying
// sync.WaitGroup counter would go negative and panic. Therefore callers must
// always call trackedWg.Add before launching the goroutine. This test
// exercises the safe pattern: Add is called on the main goroutine, the
// goroutine is launched afterwards, and Done is only reachable after Add has
// fully returned (including the wg.Add(1) at the end of Add).
func TestTrackedWgAddOrdering(t *testing.T) {
	var wg trackedWg

	// Add is called synchronously here, before the goroutine starts.
	// This ensures wg.Add(1) completes before the goroutine can call wg.Done().
	// Calling Done before Add completes would cause the underlying sync.WaitGroup
	// to go negative (counter < 0) and panic, because Add releases the mutex
	// before calling wg.Add(1), creating a window where the goroutine could
	// race ahead if it were launched prematurely.
	wg.Add("x")

	go func() {
		// By the time this goroutine runs, Add("x") has fully returned and
		// wg.Add(1) has been called, so Done("x") / wg.Done() is safe.
		wg.Done("x")
	}()

	// Must complete without panic.
	wg.Wait()

	if p := wg.Pending(); len(p) != 0 {
		t.Fatalf("expected empty Pending after Wait, got %v", p)
	}
}
