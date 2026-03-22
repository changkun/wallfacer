package handler

import (
	"sync"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/pkg/cache"
	"github.com/google/uuid"
)

func TestNewDiffCache(t *testing.T) {
	c := newDiffCache()
	if c == nil {
		t.Fatal("newDiffCache() returned nil")
	}
}

func TestDiffCacheGetMiss(t *testing.T) {
	c := newDiffCache()
	entry, ok := c.get(uuid.New())
	if ok {
		t.Error("expected miss on empty cache, got hit")
	}
	if entry.payload != nil || entry.etag != "" || entry.immutable {
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
	now := time.Now()
	clock := func() time.Time { return now }
	c := &diffCache{
		c: cache.New[uuid.UUID, diffCacheEntry](
			diffCacheTTL,
			cache.WithClock[uuid.UUID, diffCacheEntry](clock),
			cache.WithMaxSize[uuid.UUID, diffCacheEntry](maxImmutableEntries),
		),
	}
	id := uuid.New()
	c.set(id, diffCacheEntry{
		payload:   []byte(`"immutable"`),
		etag:      "etag1",
		immutable: true,
	})

	// Advance time 100 years.
	now = now.Add(100 * 365 * 24 * time.Hour)

	_, ok := c.get(id)
	if !ok {
		t.Error("immutable entry must never expire, but get() returned miss 100 years in the future")
	}
}

func TestDiffCacheTTLExpiryDirect(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	c := &diffCache{
		c: cache.New[uuid.UUID, diffCacheEntry](
			diffCacheTTL,
			cache.WithClock[uuid.UUID, diffCacheEntry](clock),
		),
	}
	id := uuid.New()
	c.set(id, diffCacheEntry{
		payload:   []byte(`"data"`),
		etag:      "etag2",
		immutable: false,
	})

	if _, ok := c.get(id); !ok {
		t.Fatal("expected hit before expiry")
	}

	now = now.Add(diffCacheTTL + time.Millisecond)

	if _, ok := c.get(id); ok {
		t.Error("expected miss after expiry, got hit")
	}
}

func TestDiffCacheTTLNotYetExpired(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	c := &diffCache{
		c: cache.New[uuid.UUID, diffCacheEntry](
			diffCacheTTL,
			cache.WithClock[uuid.UUID, diffCacheEntry](clock),
		),
	}
	id := uuid.New()
	c.set(id, diffCacheEntry{
		payload:   []byte(`"data"`),
		etag:      "etag3",
		immutable: false,
	})

	now = now.Add(diffCacheTTL - time.Millisecond)

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
	c.invalidate(uuid.New())
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

	tag1 := diffETag(payload)
	tag2 := diffETag(payload)
	if tag1 != tag2 {
		t.Errorf("diffETag is not deterministic: %q != %q", tag1, tag2)
	}

	if len(tag1) != 16 {
		t.Errorf("expected 16-char ETag, got %d chars: %q", len(tag1), tag1)
	}

	other := []byte(`{"diff":"different payload"}`)
	tag3 := diffETag(other)
	if tag1 == tag3 {
		t.Errorf("different payloads produced the same ETag: %q", tag1)
	}
}

func TestDiffCacheLRUEviction(t *testing.T) {
	c := newDiffCache()

	ids := make([]uuid.UUID, maxImmutableEntries+1)
	for i := range ids {
		ids[i] = uuid.New()
		c.set(ids[i], diffCacheEntry{
			payload:   []byte(`"data"`),
			etag:      "e",
			immutable: true,
		})
	}

	// The oldest entry (ids[0]) must have been evicted.
	if _, ok := c.get(ids[0]); ok {
		t.Error("oldest entry should have been evicted but is still retrievable")
	}

	// The newest entry must still be present.
	if _, ok := c.get(ids[maxImmutableEntries]); !ok {
		t.Error("newest entry should still be retrievable after eviction")
	}
}

func TestDiffCacheLRUResetOnDuplicateSet(t *testing.T) {
	c := newDiffCache()
	id := uuid.New()

	c.set(id, diffCacheEntry{immutable: true, payload: []byte(`"v1"`), etag: "e1"})
	c.set(id, diffCacheEntry{immutable: true, payload: []byte(`"v2"`), etag: "e2"})

	entry, ok := c.get(id)
	if !ok {
		t.Fatal("entry should be retrievable")
	}
	if string(entry.payload) != `"v2"` {
		t.Errorf("expected latest payload %q, got %q", `"v2"`, entry.payload)
	}
}

func TestDiffCacheConcurrentAccess(_ *testing.T) {
	c := newDiffCache()
	id := uuid.New()

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				c.set(id, diffCacheEntry{
					payload:   []byte(`"concurrent"`),
					etag:      "ce",
					immutable: false,
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
