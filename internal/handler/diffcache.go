package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"changkun.de/x/wallfacer/internal/pkg/cache"
	"github.com/google/uuid"
)

// diffCacheTTL is the time-to-live for cached diff entries for non-terminal tasks.
const diffCacheTTL = 10 * time.Second

// maxImmutableEntries caps the number of retained terminal-task diff entries.
// At ~16 KB max diff size this bounds worst-case memory at ~4 MB.
const maxImmutableEntries = 256

// diffCacheEntry holds a pre-serialized diff response with cache metadata.
type diffCacheEntry struct {
	payload   []byte // pre-serialized JSON response
	etag      string // hex of sha256(payload)[:16]
	immutable bool   // true for done/cancelled/archived tasks
}

// diffCache is a task-state-keyed in-memory cache for diff responses.
// Non-terminal tasks are cached for diffCacheTTL; terminal tasks are cached
// indefinitely (immutable) up to maxImmutableEntries, with oldest-first eviction.
type diffCache struct {
	c *cache.TTLCache[uuid.UUID, diffCacheEntry]
}

func newDiffCache() *diffCache {
	return newDiffCacheWithOpts()
}

func newDiffCacheWithOpts(opts ...cache.Option[uuid.UUID, diffCacheEntry]) *diffCache {
	allOpts := []cache.Option[uuid.UUID, diffCacheEntry]{
		cache.WithMaxSize[uuid.UUID, diffCacheEntry](maxImmutableEntries),
	}
	allOpts = append(allOpts, opts...)
	return &diffCache{
		c: cache.New[uuid.UUID, diffCacheEntry](diffCacheTTL, allOpts...),
	}
}

// get returns a cached entry for id if one exists and has not expired.
func (d *diffCache) get(id uuid.UUID) (diffCacheEntry, bool) {
	return d.c.Get(id)
}

// set stores an entry in the cache. Immutable entries are stored permanently
// with bounded eviction; volatile entries use the default TTL.
func (d *diffCache) set(id uuid.UUID, entry diffCacheEntry) {
	if entry.immutable {
		d.c.SetPermanent(id, entry)
	} else {
		d.c.Set(id, entry)
	}
}

// invalidate removes any cached entry for the given task.
func (d *diffCache) invalidate(id uuid.UUID) {
	d.c.Invalidate(id)
}

// diffETag computes a short ETag string for a diff payload.
func diffETag(payload []byte) string {
	h := sha256.Sum256(payload)
	return hex.EncodeToString(h[:])[:16]
}
