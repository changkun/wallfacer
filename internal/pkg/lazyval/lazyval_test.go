package lazyval

import (
	"sync"
	"sync/atomic"
	"testing"
)

// TestValue_Get verifies that Get calls the load function exactly once and
// returns the cached result on subsequent calls.
func TestValue_Get(t *testing.T) {
	calls := 0
	v := New(func() int {
		calls++
		return 42
	})

	if got := v.Get(); got != 42 {
		t.Fatalf("want 42, got %d", got)
	}
	if got := v.Get(); got != 42 {
		t.Fatalf("want 42, got %d", got)
	}
	if calls != 1 {
		t.Fatalf("load should be called once, called %d times", calls)
	}
}

// TestValue_Invalidate verifies that Invalidate clears the cache so the next
// Get re-invokes the load function with a fresh result.
func TestValue_Invalidate(t *testing.T) {
	calls := 0
	v := New(func() int {
		calls++
		return calls * 10
	})

	if got := v.Get(); got != 10 {
		t.Fatalf("first Get: want 10, got %d", got)
	}

	v.Invalidate()

	if got := v.Get(); got != 20 {
		t.Fatalf("after Invalidate: want 20, got %d", got)
	}
	if calls != 2 {
		t.Fatalf("load should be called twice, called %d times", calls)
	}
}

// TestValue_ConcurrentGet verifies that under contention from 100 goroutines,
// the load function is called exactly once (mutex serialization).
func TestValue_ConcurrentGet(t *testing.T) {
	var loadCount atomic.Int32
	v := New(func() string {
		loadCount.Add(1)
		return "hello"
	})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := v.Get(); got != "hello" {
				t.Errorf("want hello, got %q", got)
			}
		}()
	}
	wg.Wait()

	if n := loadCount.Load(); n != 1 {
		t.Fatalf("load should be called exactly once under contention, called %d times", n)
	}
}

// TestValue_ConcurrentInvalidate exercises interleaved Get and Invalidate calls
// to verify the mutex prevents data races and the final value is always valid.
func TestValue_ConcurrentInvalidate(t *testing.T) {
	var counter atomic.Int32
	v := New(func() int {
		return int(counter.Add(1))
	})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			v.Get()
		}()
		go func() {
			defer wg.Done()
			v.Invalidate()
		}()
	}
	wg.Wait()

	// After all goroutines finish, Get should still return a valid value.
	got := v.Get()
	if got <= 0 {
		t.Fatalf("expected positive value, got %d", got)
	}
}
