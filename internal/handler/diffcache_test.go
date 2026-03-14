package handler

// Note: the `now` field on diffCache was added specifically to support
// time-controlled unit tests, making it possible to simulate TTL expiry
// without real-time delays.

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewDiffCache(t *testing.T) {
	c := newDiffCache()
	if c == nil {
		t.Fatal("newDiffCache() returned nil")
	}
	if c.entries == nil {
		t.Error("entries map is nil")
	}
	if len(c.entries) != 0 {
		t.Errorf("entries map is not empty, got %d entries", len(c.entries))
	}
	if c.now == nil {
		t.Error("now func is nil")
	}
}

func TestDiffCacheGetMiss(t *testing.T) {
	c := newDiffCache()
	entry, ok := c.get(uuid.New())
	if ok {
		t.Error("expected miss on empty cache, got hit")
	}
	if entry.payload != nil || entry.etag != "" || entry.immutable || !entry.expiresAt.IsZero() {
		t.Errorf("expected zero-value entry on miss, got %+v", entry)
	}
}

func TestDiffCacheSetGetImmutable(t *testing.T) {
	c := newDiffCache()
	id := uuid.New()
	want := diffCacheEntry{
		payload:   []byte(`{"diff":"data"}`),
		etag:      "abc123",
		immutable: true,
		expiresAt: time.Time{},
	}
	c.set(id, want)

	got, ok := c.get(id)
	if !ok {
		t.Fatal("expected hit for immutable entry, got miss")
	}
	if string(got.payload) != string(want.payload) {
		t.Errorf("payload mismatch: got %q, want %q", got.payload, want.payload)
	}
	if got.etag != want.etag {
		t.Errorf("etag mismatch: got %q, want %q", got.etag, want.etag)
	}
	if !got.immutable {
		t.Error("expected immutable=true")
	}
}

func TestDiffCacheImmutableNeverExpires(t *testing.T) {
	future := time.Now().Add(100 * 365 * 24 * time.Hour) // ~100 years ahead
	c := &diffCache{
		entries: make(map[uuid.UUID]diffCacheEntry),
		now:     func() time.Time { return future },
	}
	id := uuid.New()
	c.set(id, diffCacheEntry{
		payload:   []byte(`"immutable"`),
		etag:      "etag1",
		immutable: true,
		expiresAt: time.Time{},
	})

	_, ok := c.get(id)
	if !ok {
		t.Error("immutable entry must never expire, but get() returned miss 100 years in the future")
	}
}

func TestDiffCacheTTLExpiryDirect(t *testing.T) {
	clock := time.Now()
	c := &diffCache{
		entries: make(map[uuid.UUID]diffCacheEntry),
		now:     func() time.Time { return clock },
	}
	id := uuid.New()
	c.set(id, diffCacheEntry{
		payload:   []byte(`"data"`),
		etag:      "etag2",
		immutable: false,
		expiresAt: clock.Add(diffCacheTTL),
	})

	// Entry must be present before expiry.
	if _, ok := c.get(id); !ok {
		t.Fatal("expected hit before expiry")
	}

	// Advance clock past expiresAt.
	clock = clock.Add(diffCacheTTL + time.Millisecond)

	// Entry must be gone after expiry.
	if _, ok := c.get(id); ok {
		t.Error("expected miss after expiry, got hit")
	}

	// Confirm eviction side-effect: entry must have been deleted from the map.
	c.mu.Lock()
	_, stillPresent := c.entries[id]
	c.mu.Unlock()
	if stillPresent {
		t.Error("expired entry was not evicted from the map")
	}
}

func TestDiffCacheTTLNotYetExpired(t *testing.T) {
	clock := time.Now()
	c := &diffCache{
		entries: make(map[uuid.UUID]diffCacheEntry),
		now:     func() time.Time { return clock },
	}
	id := uuid.New()
	expiresAt := clock.Add(diffCacheTTL)
	c.set(id, diffCacheEntry{
		payload:   []byte(`"data"`),
		etag:      "etag3",
		immutable: false,
		expiresAt: expiresAt,
	})

	// 1 ms before expiry — entry must still be present.
	clock = expiresAt.Add(-time.Millisecond)

	if _, ok := c.get(id); !ok {
		t.Error("expected hit 1ms before expiry, got miss")
	}
}

func TestDiffCacheInvalidate(t *testing.T) {
	c := newDiffCache()
	id := uuid.New()
	c.set(id, diffCacheEntry{
		payload:   []byte(`"x"`),
		etag:      "e",
		immutable: true,
	})

	c.invalidate(id)

	if _, ok := c.get(id); ok {
		t.Error("expected miss after invalidate, got hit")
	}

	// Invalidating an unknown ID must not panic.
	unknown := uuid.New()
	c.invalidate(unknown)
}

func TestDiffCacheInvalidateIsolation(t *testing.T) {
	c := newDiffCache()
	id1 := uuid.New()
	id2 := uuid.New()

	c.set(id1, diffCacheEntry{payload: []byte(`"a"`), etag: "e1", immutable: true})
	c.set(id2, diffCacheEntry{payload: []byte(`"b"`), etag: "e2", immutable: true})

	c.invalidate(id1)

	if _, ok := c.get(id1); ok {
		t.Error("id1 should be gone after invalidate")
	}
	if _, ok := c.get(id2); !ok {
		t.Error("id2 should still be present after invalidating id1")
	}
}

func TestDiffETag(t *testing.T) {
	payload := []byte(`{"diff":"test payload"}`)

	// Deterministic: same input → same output.
	tag1 := diffETag(payload)
	tag2 := diffETag(payload)
	if tag1 != tag2 {
		t.Errorf("diffETag is not deterministic: %q != %q", tag1, tag2)
	}

	// Exactly 16 characters.
	if len(tag1) != 16 {
		t.Errorf("expected 16-char ETag, got %d chars: %q", len(tag1), tag1)
	}

	// Different payloads must produce different ETags.
	other := []byte(`{"diff":"different payload"}`)
	tag3 := diffETag(other)
	if tag1 == tag3 {
		t.Errorf("different payloads produced the same ETag: %q", tag1)
	}
	if len(tag3) != 16 {
		t.Errorf("expected 16-char ETag for second payload, got %d chars: %q", len(tag3), tag3)
	}
}

func TestDiffCacheLRUEviction(t *testing.T) {
	c := newDiffCache()

	// Insert maxImmutableEntries + 1 distinct immutable entries.
	ids := make([]uuid.UUID, maxImmutableEntries+1)
	for i := range ids {
		ids[i] = uuid.New()
		c.set(ids[i], diffCacheEntry{
			payload:   []byte(`"data"`),
			etag:      "e",
			immutable: true,
		})
	}

	// Only maxImmutableEntries should remain.
	c.mu.Lock()
	got := len(c.entries)
	c.mu.Unlock()
	if got != maxImmutableEntries {
		t.Errorf("expected %d entries after eviction, got %d", maxImmutableEntries, got)
	}

	// The oldest entry (ids[0]) must have been evicted.
	if _, ok := c.get(ids[0]); ok {
		t.Error("oldest entry should have been evicted but is still retrievable")
	}

	// The newest entry (ids[maxImmutableEntries]) must still be present.
	if _, ok := c.get(ids[maxImmutableEntries]); !ok {
		t.Error("newest entry should still be retrievable after eviction")
	}
}

func TestDiffCacheLRUImmutableKeysLen(t *testing.T) {
	c := newDiffCache()

	// Insert exactly maxImmutableEntries entries — no eviction should occur.
	for i := 0; i < maxImmutableEntries; i++ {
		c.set(uuid.New(), diffCacheEntry{immutable: true, payload: []byte(`"x"`), etag: "e"})
	}
	c.mu.Lock()
	nKeys := len(c.immutableKeys)
	nEntries := len(c.entries)
	c.mu.Unlock()
	if nKeys != maxImmutableEntries {
		t.Errorf("immutableKeys length should be %d, got %d", maxImmutableEntries, nKeys)
	}
	if nEntries != maxImmutableEntries {
		t.Errorf("entries length should be %d, got %d", maxImmutableEntries, nEntries)
	}
}

func TestDiffCacheInvalidateRemovesFromImmutableKeys(t *testing.T) {
	c := newDiffCache()
	id := uuid.New()
	c.set(id, diffCacheEntry{immutable: true, payload: []byte(`"x"`), etag: "e"})

	c.invalidate(id)

	c.mu.Lock()
	nKeys := len(c.immutableKeys)
	c.mu.Unlock()
	if nKeys != 0 {
		t.Errorf("immutableKeys should be empty after invalidate, got len=%d", nKeys)
	}
}

func TestDiffCacheLRUNonImmutableNotTracked(t *testing.T) {
	clock := time.Now()
	c := &diffCache{
		entries:       make(map[uuid.UUID]diffCacheEntry),
		immutableKeys: make([]uuid.UUID, 0, maxImmutableEntries+1),
		now:           func() time.Time { return clock },
	}

	// Non-immutable entries must not appear in immutableKeys.
	id := uuid.New()
	c.set(id, diffCacheEntry{
		payload:   []byte(`"data"`),
		etag:      "e",
		immutable: false,
		expiresAt: clock.Add(diffCacheTTL),
	})

	c.mu.Lock()
	nKeys := len(c.immutableKeys)
	c.mu.Unlock()
	if nKeys != 0 {
		t.Errorf("non-immutable entry should not be tracked in immutableKeys, got len=%d", nKeys)
	}

	// TTL expiry still works (regression guard).
	clock = clock.Add(diffCacheTTL + time.Millisecond)
	if _, ok := c.get(id); ok {
		t.Error("non-immutable entry should have expired, but get() returned a hit")
	}
}

func TestDiffCacheLRUResetOnDuplicateSet(t *testing.T) {
	c := newDiffCache()
	id := uuid.New()

	// Set the same immutable ID twice — immutableKeys must not grow.
	c.set(id, diffCacheEntry{immutable: true, payload: []byte(`"v1"`), etag: "e1"})
	c.set(id, diffCacheEntry{immutable: true, payload: []byte(`"v2"`), etag: "e2"})

	c.mu.Lock()
	nKeys := len(c.immutableKeys)
	c.mu.Unlock()
	if nKeys != 1 {
		t.Errorf("duplicate set should not append to immutableKeys; want 1, got %d", nKeys)
	}

	// Latest value should be returned.
	entry, ok := c.get(id)
	if !ok {
		t.Fatal("entry should be retrievable")
	}
	if string(entry.payload) != `"v2"` {
		t.Errorf("expected latest payload %q, got %q", `"v2"`, entry.payload)
	}
}

func TestDiffCacheConcurrentAccess(t *testing.T) {
	c := newDiffCache()
	id := uuid.New()

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				c.set(id, diffCacheEntry{
					payload:   []byte(`"concurrent"`),
					etag:      "ce",
					immutable: false,
					expiresAt: time.Now().Add(diffCacheTTL),
				})
			} else {
				if i%4 == 1 {
					c.get(id)
				} else {
					c.invalidate(id)
				}
			}
		}()
	}

	wg.Wait()
}
