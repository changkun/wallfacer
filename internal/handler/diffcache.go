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

// maxImmutableEntries caps the number of retained terminal-task diff entries.
// At ~16 KB max diff size this bounds worst-case memory at ~4 MB.
const maxImmutableEntries = 256

// diffCacheEntry holds a pre-serialized diff response with cache metadata.
type diffCacheEntry struct {
	payload   []byte    // pre-serialized JSON response
	etag      string    // hex of sha256(payload)[:16]
	immutable bool      // true for done/cancelled/archived tasks
	expiresAt time.Time // zero for immutable entries
}

// diffCache is a task-state-keyed in-memory cache for diff responses.
// Non-terminal tasks are cached for diffCacheTTL; terminal tasks are cached
// indefinitely (immutable) up to maxImmutableEntries, with oldest-first eviction.
type diffCache struct {
	mu            sync.Mutex
	entries       map[uuid.UUID]diffCacheEntry
	immutableKeys []uuid.UUID      // insertion order, for oldest-first eviction
	now           func() time.Time // injectable clock for testing
}

func newDiffCache() *diffCache {
	return &diffCache{
		entries:       make(map[uuid.UUID]diffCacheEntry),
		immutableKeys: make([]uuid.UUID, 0, maxImmutableEntries+1),
		now:           time.Now,
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

// set stores an entry in the cache. For immutable entries, it tracks insertion
// order and evicts the oldest when maxImmutableEntries is exceeded.
func (c *diffCache) set(id uuid.UUID, entry diffCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry.immutable {
		if _, exists := c.entries[id]; !exists {
			c.immutableKeys = append(c.immutableKeys, id)
			if len(c.immutableKeys) > maxImmutableEntries {
				oldest := c.immutableKeys[0]
				c.immutableKeys = c.immutableKeys[1:]
				delete(c.entries, oldest)
			}
		}
	}
	c.entries[id] = entry
}

// invalidate removes any cached entry for the given task.
func (c *diffCache) invalidate(id uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[id]; ok && e.immutable {
		for i, k := range c.immutableKeys {
			if k == id {
				c.immutableKeys = append(c.immutableKeys[:i], c.immutableKeys[i+1:]...)
				break
			}
		}
	}
	delete(c.entries, id)
}

// diffETag computes a short ETag string for a diff payload.
func diffETag(payload []byte) string {
	h := sha256.Sum256(payload)
	return hex.EncodeToString(h[:])[:16]
}
