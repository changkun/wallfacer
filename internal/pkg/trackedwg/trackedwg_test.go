package trackedwg

import (
	"sync"
	"testing"
	"time"
)

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
