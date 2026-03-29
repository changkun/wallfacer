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
	permanent bool           // permanent entries never expire by TTL
	expiresAt time.Time      // zero for permanent entries
	elem      *list.Element  // position in LRU list (nil for non-permanent)
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

// Set stores a value with the cache's default TTL.
func (c *TTLCache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = entry[K, V]{
		key:       key,
		value:     value,
		expiresAt: c.now().Add(c.defaultTTL),
	}
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
