package handler

import (
	"time"

	"changkun.de/x/wallfacer/internal/gitutil"
	"changkun.de/x/wallfacer/internal/pkg/cache"
)

// commitsBehindResult stores a cached CommitsBehind outcome.
type commitsBehindResult struct {
	n   int
	err error
}

// commitsBehindCache is a short-lived in-memory cache for gitutil.CommitsBehind
// results, shared across the three autopilot watchers that poll waiting tasks:
// checkAndSyncWaitingTasks, tryAutoTest, and tryAutoSubmit. Each CommitsBehind
// call runs 3–6 git subprocesses, so deduplication within a polling window
// substantially reduces subprocess overhead when N waiting tasks exist.
type commitsBehindCache struct {
	c *cache.TTLCache[string, commitsBehindResult]
}

func newCommitsBehindCache(ttl time.Duration) *commitsBehindCache {
	return &commitsBehindCache{
		c: cache.New[string, commitsBehindResult](ttl),
	}
}

func (c *commitsBehindCache) key(repoPath, worktreePath string) string {
	return repoPath + "\x00" + worktreePath
}

// get returns a cached result if one exists and has not expired.
func (c *commitsBehindCache) get(repoPath, worktreePath string) (int, bool, error) {
	r, ok := c.c.Get(c.key(repoPath, worktreePath))
	if !ok {
		return 0, false, nil
	}
	return r.n, true, r.err
}

// set stores a result in the cache with the configured TTL.
func (c *commitsBehindCache) set(repoPath, worktreePath string, n int, err error) {
	c.c.Set(c.key(repoPath, worktreePath), commitsBehindResult{n: n, err: err})
}

// cachedCommitsBehind returns a cached result if available, otherwise calls
// gitutil.CommitsBehind, caches the result, and returns it.
func (c *commitsBehindCache) cachedCommitsBehind(repoPath, worktreePath string) (int, error) {
	if n, ok, err := c.get(repoPath, worktreePath); ok {
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
	c.c.Invalidate(c.key(repoPath, worktreePath))
}
