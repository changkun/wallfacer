// Package cache provides a generic thread-safe in-memory cache with TTL
// expiration and optional permanent entries with bounded LRU eviction.
package cache

import (
	"sync"
	"time"
)

type entry[V any] struct {
	value     V
	permanent bool      // permanent entries never expire by TTL
	expiresAt time.Time // zero for permanent entries
}

// TTLCache is a generic thread-safe key-value cache with per-entry expiration.
// Non-permanent entries expire after the configured default TTL. Permanent
// entries (inserted via [SetPermanent]) never expire by TTL but are subject to
// bounded eviction when MaxSize is set.
type TTLCache[K comparable, V any] struct {
	mu            sync.Mutex
	entries       map[K]entry[V]
	permanentKeys []K // insertion order, for oldest-first eviction
	defaultTTL    time.Duration
	maxSize       int // max permanent entries (0 = unlimited)
	now           func() time.Time
}

// Option configures a [TTLCache].
type Option[K comparable, V any] func(*TTLCache[K, V])

// WithClock sets an injectable time source (default: time.Now).
func WithClock[K comparable, V any](now func() time.Time) Option[K, V] {
	return func(c *TTLCache[K, V]) { c.now = now }
}

// WithMaxSize sets the maximum number of permanent entries. When exceeded,
// the oldest permanent entry is evicted. 0 means unlimited. This limit does
// NOT apply to TTL-based entries.
func WithMaxSize[K comparable, V any](n int) Option[K, V] {
	return func(c *TTLCache[K, V]) { c.maxSize = n }
}

// New creates a TTLCache with the given default TTL and options.
func New[K comparable, V any](defaultTTL time.Duration, opts ...Option[K, V]) *TTLCache[K, V] {
	c := &TTLCache[K, V]{
		entries:    make(map[K]entry[V]),
		defaultTTL: defaultTTL,
		now:        time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Get returns the value and true if the key exists and has not expired.
// Expired non-permanent entries are evicted on access.
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
	return e.value, true
}

// Set stores a value with the cache's default TTL.
func (c *TTLCache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = entry[V]{
		value:     value,
		expiresAt: c.now().Add(c.defaultTTL),
	}
}

// SetPermanent stores a value that never expires by TTL. If MaxSize is
// configured, the oldest permanent entry is evicted when the cap is exceeded.
func (c *TTLCache[K, V]) SetPermanent(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; !exists {
		c.permanentKeys = append(c.permanentKeys, key)
		if c.maxSize > 0 && len(c.permanentKeys) > c.maxSize {
			oldest := c.permanentKeys[0]
			c.permanentKeys = c.permanentKeys[1:]
			delete(c.entries, oldest)
		}
	}
	c.entries[key] = entry[V]{
		value:     value,
		permanent: true,
	}
}

// Invalidate removes an entry regardless of its TTL or permanence.
func (c *TTLCache[K, V]) Invalidate(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok && e.permanent {
		for i, k := range c.permanentKeys {
			if k == key {
				c.permanentKeys = append(c.permanentKeys[:i], c.permanentKeys[i+1:]...)
				break
			}
		}
	}
	delete(c.entries, key)
}
