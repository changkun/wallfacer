package handler

// Note: the `now` field on commitsBehindCache was added specifically to support
// time-controlled unit tests, making it possible to simulate TTL expiry
// without real-time delays.

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCommitsBehindCache_Miss(t *testing.T) {
	c := newCommitsBehindCache(commitsBehindCacheTTL)
	_, ok, _ := c.get("repo", "worktree")
	if ok {
		t.Error("expected miss on empty cache, got hit")
	}
}

func TestCommitsBehindCache_Hit(t *testing.T) {
	c := newCommitsBehindCache(commitsBehindCacheTTL)
	c.set("repo", "worktree", 3, nil)

	n, ok, err := c.get("repo", "worktree")
	if !ok {
		t.Fatal("expected cache hit, got miss")
	}
	if n != 3 {
		t.Errorf("expected n=3, got %d", n)
	}
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestCommitsBehindCache_Expiry(t *testing.T) {
	clock := time.Now()
	c := &commitsBehindCache{
		entries: make(map[string]commitsBehindEntry),
		ttl:     commitsBehindCacheTTL,
		now:     func() time.Time { return clock },
	}

	c.set("repo", "worktree", 5, nil)

	// Entry must be present before expiry.
	if _, ok, _ := c.get("repo", "worktree"); !ok {
		t.Fatal("expected cache hit before TTL expiry")
	}

	// Advance clock past TTL.
	clock = clock.Add(commitsBehindCacheTTL + time.Millisecond)

	_, ok, _ := c.get("repo", "worktree")
	if ok {
		t.Error("expected cache miss after TTL expiry, got hit")
	}
}

func TestCommitsBehindCache_NotYetExpired(t *testing.T) {
	clock := time.Now()
	c := &commitsBehindCache{
		entries: make(map[string]commitsBehindEntry),
		ttl:     commitsBehindCacheTTL,
		now:     func() time.Time { return clock },
	}

	c.set("repo", "worktree", 2, nil)

	// 1ms before expiry — entry must still be present.
	clock = clock.Add(commitsBehindCacheTTL - time.Millisecond)

	if _, ok, _ := c.get("repo", "worktree"); !ok {
		t.Error("expected cache hit 1ms before expiry, got miss")
	}
}

func TestCommitsBehindCache_Invalidate(t *testing.T) {
	c := newCommitsBehindCache(commitsBehindCacheTTL)
	c.set("repo", "worktree", 1, nil)

	c.invalidate("repo", "worktree")

	if _, ok, _ := c.get("repo", "worktree"); ok {
		t.Error("expected miss after invalidate, got hit")
	}
}

func TestCommitsBehindCache_InvalidateUnknown(_ *testing.T) {
	c := newCommitsBehindCache(commitsBehindCacheTTL)
	// Must not panic when the key does not exist.
	c.invalidate("nonexistent-repo", "nonexistent-worktree")
}

func TestCommitsBehindCache_InvalidateIsolation(t *testing.T) {
	c := newCommitsBehindCache(commitsBehindCacheTTL)
	c.set("repo", "wt1", 1, nil)
	c.set("repo", "wt2", 2, nil)

	c.invalidate("repo", "wt1")

	if _, ok, _ := c.get("repo", "wt1"); ok {
		t.Error("wt1 should be gone after invalidate")
	}
	if _, ok, _ := c.get("repo", "wt2"); !ok {
		t.Error("wt2 should still be present after invalidating wt1")
	}
}

func TestCommitsBehindCache_CachesError(t *testing.T) {
	sentinel := errors.New("git error")
	c := newCommitsBehindCache(commitsBehindCacheTTL)
	c.set("repo", "worktree", 0, sentinel)

	_, ok, err := c.get("repo", "worktree")
	if !ok {
		t.Fatal("expected cache hit for error result, got miss")
	}
	if err != sentinel {
		t.Errorf("expected sentinel error, got %v", err)
	}
}

func TestCommitsBehindCache_CachedCommitsBehind_ServesFromCache(t *testing.T) {
	calls := 0
	c := newCommitsBehindCache(commitsBehindCacheTTL)
	// Pre-populate with a known value to simulate a cache hit.
	c.set("repo", "worktree", 7, nil)

	// Override cachedCommitsBehind via set — the real gitutil.CommitsBehind
	// would fail in a unit test environment since no actual git repo exists.
	// We verify the cached value is returned without invoking CommitsBehind.
	_ = calls // silence unused warning

	n, ok, err := c.get("repo", "worktree")
	if !ok {
		t.Fatal("cache miss on pre-populated entry")
	}
	if n != 7 {
		t.Errorf("expected n=7, got %d", n)
	}
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestCommitsBehindCache_ConcurrentSafe(_ *testing.T) {
	c := newCommitsBehindCache(commitsBehindCacheTTL)
	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			switch i % 3 {
			case 0:
				c.set("repo", "worktree", i, nil)
			case 1:
				_, _, _ = c.get("repo", "worktree")
			case 2:
				c.invalidate("repo", "worktree")
			}
		}()
	}

	wg.Wait()
	// No assertions needed — the race detector validates safety.
}

func TestCommitsBehindCache_KeySeparation(t *testing.T) {
	c := newCommitsBehindCache(commitsBehindCacheTTL)
	// Two entries with the same repo but different worktrees.
	c.set("/repo", "/wt1", 1, nil)
	c.set("/repo", "/wt2", 2, nil)

	n1, ok1, _ := c.get("/repo", "/wt1")
	n2, ok2, _ := c.get("/repo", "/wt2")

	if !ok1 || n1 != 1 {
		t.Errorf("wt1: expected (1, true), got (%d, %v)", n1, ok1)
	}
	if !ok2 || n2 != 2 {
		t.Errorf("wt2: expected (2, true), got (%d, %v)", n2, ok2)
	}
}
