package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/google/uuid"
)

// diffCacheTTL is the time-to-live for cached diff entries for non-terminal tasks.
const diffCacheTTL = 10 * time.Second

// diffCacheEntry holds a pre-serialized diff response with cache metadata.
type diffCacheEntry struct {
	payload   []byte    // pre-serialized JSON response
	etag      string    // hex of sha256(payload)[:16]
	immutable bool      // true for done/cancelled/archived tasks
	expiresAt time.Time // zero for immutable entries
}

// diffCache is a task-state-keyed in-memory cache for diff responses.
// Non-terminal tasks are cached for diffCacheTTL; terminal tasks are cached
// indefinitely (immutable).
type diffCache struct {
	mu      sync.Mutex
	entries map[uuid.UUID]diffCacheEntry
	now     func() time.Time // injectable clock for testing
}

func newDiffCache() *diffCache {
	return &diffCache{
		entries: make(map[uuid.UUID]diffCacheEntry),
		now:     time.Now,
	}
}

// get returns a cached entry for id if one exists and has not expired.
// Expired non-immutable entries are evicted on access.
func (c *diffCache) get(id uuid.UUID) (diffCacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[id]
	if !ok {
		return diffCacheEntry{}, false
	}
	if !entry.immutable && c.now().After(entry.expiresAt) {
		delete(c.entries, id)
		return diffCacheEntry{}, false
	}
	return entry, true
}

// set stores an entry in the cache.
func (c *diffCache) set(id uuid.UUID, entry diffCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[id] = entry
}

// invalidate removes any cached entry for the given task.
func (c *diffCache) invalidate(id uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, id)
}

// diffETag computes a short ETag string for a diff payload.
func diffETag(payload []byte) string {
	h := sha256.Sum256(payload)
	return hex.EncodeToString(h[:])[:16]
}
