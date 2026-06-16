// Package cache provides a generic thread-safe in-memory cache with TTL
// expiration and optional permanent entries with bounded LRU eviction.
package cache

import (
	"container/list"
	"sync"
	"time"
)

// entry holds a cached value along with its expiration metadata.
type entry[K comparable, V any] struct {
	key       K
	value     V
	permanent bool          // permanent entries never expire by TTL
	expiresAt time.Time     // zero for permanent entries
	elem      *list.Element // position in LRU list (nil for non-permanent)
}

// TTLCache is a generic thread-safe key-value cache with per-entry expiration.
// Non-permanent entries expire after the configured default TTL. Permanent
// entries (inserted via [SetPermanent]) never expire by TTL but are subject to
// bounded LRU eviction when MaxSize is set. Accessing a permanent entry via
// [Get] promotes it to most-recently-used.
type TTLCache[K comparable, V any] struct {
	mu         sync.Mutex
	entries    map[K]entry[K, V]
	lru        *list.List // front = least recently used, back = most recently used
	defaultTTL time.Duration
	maxSize    int // max permanent entries (0 = unlimited)
	now        func() time.Time
}

// sweepThreshold is the entry-count high-water mark above which Set
// opportunistically reclaims expired non-permanent entries. Below it the
// map is small enough that lazy eviction on Get is sufficient.
const sweepThreshold = 256

// sweepBudget caps how many entries a single Set scans for expiry, so the
// hot insert path stays O(1) amortized rather than O(n) per call.
const sweepBudget = 8

// Option configures a [TTLCache].
type Option[K comparable, V any] func(*TTLCache[K, V])

// WithClock sets an injectable time source (default: time.Now).
func WithClock[K comparable, V any](now func() time.Time) Option[K, V] {
	return func(c *TTLCache[K, V]) { c.now = now }
}

// WithMaxSize sets the maximum number of permanent entries. When exceeded,
// the least recently used permanent entry is evicted. 0 means unlimited.
// This limit does NOT apply to TTL-based entries.
func WithMaxSize[K comparable, V any](n int) Option[K, V] {
	return func(c *TTLCache[K, V]) { c.maxSize = n }
}

// New creates a TTLCache with the given default TTL and options.
func New[K comparable, V any](defaultTTL time.Duration, opts ...Option[K, V]) *TTLCache[K, V] {
	c := &TTLCache[K, V]{
		entries:    make(map[K]entry[K, V]),
		lru:        list.New(),
		defaultTTL: defaultTTL,
		now:        time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Get returns the value and true if the key exists and has not expired.
// Expired non-permanent entries are evicted on access. Permanent entries
// are promoted to most-recently-used on access.
func (c *TTLCache[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		var zero V
		return zero, false
	}
	if !e.permanent && c.now().After(e.expiresAt) {
		delete(c.entries, key)
		var zero V
		return zero, false
	}
	if e.elem != nil {
		c.lru.MoveToBack(e.elem)
	}
	return e.value, true
}

// Set stores a value with the cache's default TTL. If the key was previously a
// permanent entry, its LRU element is removed so it no longer counts against
// MaxSize and cannot be double-tracked by a later SetPermanent.
func (c *TTLCache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok && e.elem != nil {
		c.lru.Remove(e.elem)
	}
	c.sweepExpiredLocked()
	c.entries[key] = entry[K, V]{
		key:       key,
		value:     value,
		expiresAt: c.now().Add(c.defaultTTL),
	}
}

// sweepExpiredLocked opportunistically reclaims expired non-permanent entries
// so their map slots do not leak when the same key is never read again. It is
// bounded: it does nothing until the map exceeds sweepThreshold, and it scans
// at most sweepBudget entries per call, keeping the insert path O(1) amortized.
// Permanent and live entries are left untouched. The caller must hold c.mu.
func (c *TTLCache[K, V]) sweepExpiredLocked() {
	if len(c.entries) <= sweepThreshold {
		return
	}
	now := c.now()
	scanned := 0
	for k, e := range c.entries {
		if scanned >= sweepBudget {
			break
		}
		scanned++
		if !e.permanent && now.After(e.expiresAt) {
			delete(c.entries, k)
		}
	}
}

// Len returns the current number of entries (both live and not-yet-reclaimed
// expired non-permanent entries). Intended for tests and diagnostics.
func (c *TTLCache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// SetPermanent stores a value that never expires by TTL. If MaxSize is
// configured, the least recently used permanent entry is evicted when the
// cap is exceeded.
func (c *TTLCache[K, V]) SetPermanent(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	existing, exists := c.entries[key]
	if exists && existing.elem != nil {
		// Already tracked — move to back (most recently used) and update value.
		c.lru.MoveToBack(existing.elem)
	} else {
		// New permanent entry — append to back of LRU list.
		elem := c.lru.PushBack(key)
		existing.elem = elem
		if c.maxSize > 0 && c.lru.Len() > c.maxSize {
			// Evict the least recently used permanent entry (front of list).
			front := c.lru.Front()
			evictKey := front.Value.(K)
			c.lru.Remove(front)
			delete(c.entries, evictKey)
		}
	}

	c.entries[key] = entry[K, V]{
		key:       key,
		value:     value,
		permanent: true,
		elem:      existing.elem,
	}
}

// Invalidate removes an entry regardless of its TTL or permanence.
func (c *TTLCache[K, V]) Invalidate(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok {
		if e.elem != nil {
			c.lru.Remove(e.elem)
		}
		delete(c.entries, key)
	}
}
