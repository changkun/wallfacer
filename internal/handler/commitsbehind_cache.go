package handler

import (
	"sync"
	"time"

	"changkun.de/x/wallfacer/internal/gitutil"
)

// commitsBehindCacheTTL is the time-to-live for cached CommitsBehind results.
// It is shorter than the 30-second poll interval so stale results don't persist
// across two polling cycles, but long enough for all three watchers
// (checkAndSyncWaitingTasks, tryAutoTest, tryAutoSubmit) to share a single
// result within one polling window.
const commitsBehindCacheTTL = 20 * time.Second

// commitsBehindEntry is a single cached result keyed by (repoPath, worktreePath).
type commitsBehindEntry struct {
	n         int
	err       error
	expiresAt time.Time
}

// commitsBehindCache is a short-lived in-memory cache for gitutil.CommitsBehind
// results, shared across the three autopilot watchers that poll waiting tasks:
// checkAndSyncWaitingTasks, tryAutoTest, and tryAutoSubmit. Each CommitsBehind
// call runs 3–6 git subprocesses, so deduplication within a polling window
// substantially reduces subprocess overhead when N waiting tasks exist.
//
// The now field is injectable for deterministic unit testing.
type commitsBehindCache struct {
	mu      sync.Mutex
	entries map[string]commitsBehindEntry // key: repoPath + "\x00" + worktreePath
	ttl     time.Duration
	now     func() time.Time // injectable for tests
}

func newCommitsBehindCache(ttl time.Duration) *commitsBehindCache {
	return &commitsBehindCache{
		entries: make(map[string]commitsBehindEntry),
		ttl:     ttl,
		now:     time.Now,
	}
}

func (c *commitsBehindCache) key(repoPath, worktreePath string) string {
	return repoPath + "\x00" + worktreePath
}

// get returns a cached result if one exists and has not expired.
func (c *commitsBehindCache) get(repoPath, worktreePath string) (int, error, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[c.key(repoPath, worktreePath)]
	if !ok || c.now().After(e.expiresAt) {
		return 0, nil, false
	}
	return e.n, e.err, true
}

// set stores a result in the cache with the configured TTL.
func (c *commitsBehindCache) set(repoPath, worktreePath string, n int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]commitsBehindEntry)
	}
	c.entries[c.key(repoPath, worktreePath)] = commitsBehindEntry{
		n:         n,
		err:       err,
		expiresAt: c.now().Add(c.ttl),
	}
}

// cachedCommitsBehind returns a cached result if available, otherwise calls
// gitutil.CommitsBehind, caches the result, and returns it.
func (c *commitsBehindCache) cachedCommitsBehind(repoPath, worktreePath string) (int, error) {
	if n, err, ok := c.get(repoPath, worktreePath); ok {
		return n, err
	}
	n, err := gitutil.CommitsBehind(repoPath, worktreePath)
	c.set(repoPath, worktreePath, n, err)
	return n, err
}

// invalidate removes a single entry from the cache. It is a no-op when the
// entry does not exist. Call this after sync or rebase operations that change
// a worktree's HEAD so subsequent calls observe the new state.
func (c *commitsBehindCache) invalidate(repoPath, worktreePath string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, c.key(repoPath, worktreePath))
}
