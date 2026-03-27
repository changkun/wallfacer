package trackedwg

import (
	"sync"
	"testing"
	"time"
)

// TestWaitGroup_Sequential verifies basic Add/Done/Wait lifecycle and that
// Pending correctly reflects the label before and after Done.
func TestWaitGroup_Sequential(t *testing.T) {
	var wg WaitGroup

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

// TestWaitGroup_MultipleDistinctLabels verifies that Pending returns all
// distinct labels in sorted order.
func TestWaitGroup_MultipleDistinctLabels(t *testing.T) {
	var wg WaitGroup

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

// TestWaitGroup_RepeatedLabel verifies that duplicate labels are displayed with
// a multiplicity suffix (e.g. "foo x2") and correctly decremented.
func TestWaitGroup_RepeatedLabel(t *testing.T) {
	var wg WaitGroup

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

// TestWaitGroup_WaitBlocks verifies that Wait actually blocks until Done is called.
func TestWaitGroup_WaitBlocks(t *testing.T) {
	var wg WaitGroup
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

// TestWaitGroup_ConcurrentAddDone stress-tests that concurrent Add/Done calls
// from many goroutines do not race and resolve to an empty Pending set.
func TestWaitGroup_ConcurrentAddDone(t *testing.T) {
	var wg WaitGroup
	const n = 50

	var start sync.WaitGroup
	start.Add(n)

	for range n {
		go func() {
			wg.Add("task")
			start.Done()
			time.Sleep(time.Millisecond)
			wg.Done("task")
		}()
	}

	start.Wait()
	wg.Wait()

	if p := wg.Pending(); len(p) != 0 {
		t.Fatalf("expected empty Pending after concurrent Wait, got %v", p)
	}
}

// TestWaitGroup_Go verifies that the Go convenience method launches a tracked
// goroutine that cleans up after itself.
func TestWaitGroup_Go(t *testing.T) {
	var wg WaitGroup
	done := make(chan struct{})

	wg.Go("work", func() {
		close(done)
	})

	// Verify label is tracked.
	<-done // wait for goroutine body to execute
	// Give a moment for Done to fire.
	wg.Wait()

	if p := wg.Pending(); len(p) != 0 {
		t.Fatalf("expected empty Pending after Go+Wait, got %v", p)
	}
}

// TestWaitGroup_Go_TracksLabel verifies that a goroutine launched via Go is
// visible in Pending while it is still running.
func TestWaitGroup_Go_TracksLabel(t *testing.T) {
	var wg WaitGroup
	started := make(chan struct{})
	release := make(chan struct{})

	wg.Go("blocker", func() {
		close(started)
		<-release
	})

	<-started
	pending := wg.Pending()
	if len(pending) != 1 || pending[0] != "blocker" {
		t.Fatalf("expected [blocker] in Pending, got %v", pending)
	}

	close(release)
	wg.Wait()
}

// TestWaitGroup_AddOrdering verifies that Add must be called before the
// goroutine starts, ensuring Wait does not return prematurely.
func TestWaitGroup_AddOrdering(t *testing.T) {
	var wg WaitGroup

	wg.Add("x")

	go func() {
		wg.Done("x")
	}()

	wg.Wait()

	if p := wg.Pending(); len(p) != 0 {
		t.Fatalf("expected empty Pending after Wait, got %v", p)
	}
}
