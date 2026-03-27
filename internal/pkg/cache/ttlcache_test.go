package cache

import (
	"sync"
	"testing"
	"time"
)

// TestTTLCache_SetGet verifies basic set-then-get round-trip returns the stored value.
func TestTTLCache_SetGet(t *testing.T) {
	c := New[string, int](time.Minute)
	c.Set("a", 1)
	v, ok := c.Get("a")
	if !ok || v != 1 {
		t.Fatalf("Get(a) = (%d, %v), want (1, true)", v, ok)
	}
}

// TestTTLCache_Miss verifies that Get returns false for a key that was never set.
func TestTTLCache_Miss(t *testing.T) {
	c := New[string, int](time.Minute)
	_, ok := c.Get("missing")
	if ok {
		t.Fatal("expected miss for nonexistent key")
	}
}

// TestTTLCache_Expiry verifies that entries become inaccessible after their TTL expires,
// using an injectable clock to advance time deterministically.
func TestTTLCache_Expiry(t *testing.T) {
	now := time.Now()
	c := New[string, int](10*time.Millisecond, WithClock[string, int](func() time.Time { return now }))

	c.Set("k", 42)
	if v, ok := c.Get("k"); !ok || v != 42 {
		t.Fatalf("expected hit before expiry, got (%d, %v)", v, ok)
	}

	now = now.Add(20 * time.Millisecond)
	if _, ok := c.Get("k"); ok {
		t.Fatal("expected miss after expiry")
	}
}

// TestTTLCache_SetPermanent verifies that permanent entries survive past the TTL.
func TestTTLCache_SetPermanent(t *testing.T) {
	now := time.Now()
	c := New[string, int](10*time.Millisecond, WithClock[string, int](func() time.Time { return now }))

	c.SetPermanent("p", 99)

	// Advance time way past TTL.
	now = now.Add(time.Hour)

	v, ok := c.Get("p")
	if !ok || v != 99 {
		t.Fatalf("permanent entry should not expire, got (%d, %v)", v, ok)
	}
}

// TestTTLCache_MaxSize_EvictsOldest verifies that exceeding MaxSize evicts
// the oldest permanent entry (FIFO order).
func TestTTLCache_MaxSize_EvictsOldest(t *testing.T) {
	c := New[string, int](time.Minute, WithMaxSize[string, int](2))

	c.SetPermanent("a", 1)
	c.SetPermanent("b", 2)
	c.SetPermanent("c", 3) // should evict "a"

	if _, ok := c.Get("a"); ok {
		t.Fatal("expected 'a' to be evicted")
	}
	if v, ok := c.Get("b"); !ok || v != 2 {
		t.Fatalf("expected 'b' to survive, got (%d, %v)", v, ok)
	}
	if v, ok := c.Get("c"); !ok || v != 3 {
		t.Fatalf("expected 'c' to exist, got (%d, %v)", v, ok)
	}
}

// TestTTLCache_MaxSize_UpdateDoesNotEvict verifies that updating an existing
// permanent entry does not count as a new insertion for eviction purposes.
func TestTTLCache_MaxSize_UpdateDoesNotEvict(t *testing.T) {
	c := New[string, int](time.Minute, WithMaxSize[string, int](2))

	c.SetPermanent("a", 1)
	c.SetPermanent("b", 2)
	c.SetPermanent("a", 10) // update, not a new entry

	if v, ok := c.Get("a"); !ok || v != 10 {
		t.Fatalf("expected updated 'a'=10, got (%d, %v)", v, ok)
	}
	if _, ok := c.Get("b"); !ok {
		t.Fatal("expected 'b' to survive update of 'a'")
	}
}

// TestTTLCache_Invalidate verifies that Invalidate removes a TTL-based entry.
func TestTTLCache_Invalidate(t *testing.T) {
	c := New[string, int](time.Minute)

	c.Set("k", 1)
	c.Invalidate("k")
	if _, ok := c.Get("k"); ok {
		t.Fatal("expected miss after invalidate")
	}
}

// TestTTLCache_Invalidate_Permanent verifies that Invalidate removes a permanent
// entry and also cleans it from the permanentKeys tracking slice, so subsequent
// insertions up to MaxSize do not trigger spurious evictions.
func TestTTLCache_Invalidate_Permanent(t *testing.T) {
	c := New[string, int](time.Minute, WithMaxSize[string, int](10))

	c.SetPermanent("p", 1)
	c.Invalidate("p")
	if _, ok := c.Get("p"); ok {
		t.Fatal("expected miss after invalidating permanent entry")
	}

	// Verify permanent key was removed from tracking.
	// Adding maxSize entries should not evict anything spuriously.
	for i := range 10 {
		c.SetPermanent(string(rune('A'+i)), i)
	}
	// All should exist.
	for i := range 10 {
		if _, ok := c.Get(string(rune('A' + i))); !ok {
			t.Fatalf("expected key %c to exist", 'A'+i)
		}
	}
}

// TestTTLCache_Concurrent exercises concurrent Set and Get operations to verify
// the mutex-based thread safety does not cause data races.
func TestTTLCache_Concurrent(_ *testing.T) {
	c := New[int, int](time.Minute)
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n * 2)

	for i := range n {
		go func(i int) {
			defer wg.Done()
			c.Set(i, i*10)
		}(i)
		go func(i int) {
			defer wg.Done()
			c.Get(i)
		}(i)
	}
	wg.Wait()
}
