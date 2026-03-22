package handler

import (
	"errors"
	"sync"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/pkg/cache"
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
	now := time.Now()
	c := &commitsBehindCache{
		c: cache.New[string, commitsBehindResult](
			commitsBehindCacheTTL,
			cache.WithClock[string, commitsBehindResult](func() time.Time { return now }),
		),
	}

	c.set("repo", "worktree", 5, nil)

	if _, ok, _ := c.get("repo", "worktree"); !ok {
		t.Fatal("expected cache hit before TTL expiry")
	}

	now = now.Add(commitsBehindCacheTTL + time.Millisecond)

	_, ok, _ := c.get("repo", "worktree")
	if ok {
		t.Error("expected cache miss after TTL expiry, got hit")
	}
}

func TestCommitsBehindCache_NotYetExpired(t *testing.T) {
	now := time.Now()
	c := &commitsBehindCache{
		c: cache.New[string, commitsBehindResult](
			commitsBehindCacheTTL,
			cache.WithClock[string, commitsBehindResult](func() time.Time { return now }),
		),
	}

	c.set("repo", "worktree", 2, nil)

	now = now.Add(commitsBehindCacheTTL - time.Millisecond)

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
	c := newCommitsBehindCache(commitsBehindCacheTTL)
	c.set("repo", "worktree", 7, nil)

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

	for i := range goroutines {
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
}

func TestCommitsBehindCache_KeySeparation(t *testing.T) {
	c := newCommitsBehindCache(commitsBehindCacheTTL)
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
