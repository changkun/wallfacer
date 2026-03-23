package syncmap

import (
	"fmt"
	"sync"
	"testing"
)

func TestMap_StoreLoad(t *testing.T) {
	var m Map[string, int]

	m.Store("a", 1)
	got, ok := m.Load("a")
	if !ok || got != 1 {
		t.Fatalf("Load(a): want (1, true), got (%d, %v)", got, ok)
	}
}

func TestMap_LoadMissing(t *testing.T) {
	var m Map[string, int]

	got, ok := m.Load("missing")
	if ok {
		t.Fatalf("Load(missing): want ok=false, got ok=true val=%d", got)
	}
	if got != 0 {
		t.Fatalf("Load(missing): want zero value 0, got %d", got)
	}
}

func TestMap_Delete(t *testing.T) {
	var m Map[string, string]

	m.Store("k", "v")
	m.Delete("k")

	_, ok := m.Load("k")
	if ok {
		t.Fatal("expected entry to be deleted")
	}
}

func TestMap_Range(t *testing.T) {
	var m Map[int, string]

	m.Store(1, "one")
	m.Store(2, "two")
	m.Store(3, "three")

	seen := map[int]string{}
	m.Range(func(k int, v string) bool {
		seen[k] = v
		return true
	})

	if len(seen) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(seen))
	}
}

func TestMap_RangeEarlyStop(t *testing.T) {
	var m Map[int, int]
	for i := range 5 {
		m.Store(i, i)
	}

	count := 0
	m.Range(func(_, _ int) bool {
		count++
		return false
	})
	if count != 1 {
		t.Fatalf("expected Range to stop after 1, got %d", count)
	}
}

func TestMap_Concurrent(t *testing.T) {
	var m Map[int, string]
	var wg sync.WaitGroup

	const n = 100
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.Store(i, fmt.Sprintf("val-%d", i))
		}(i)
	}
	wg.Wait()

	for i := range n {
		v, ok := m.Load(i)
		if !ok {
			t.Errorf("key %d not found", i)
			continue
		}
		if want := fmt.Sprintf("val-%d", i); v != want {
			t.Errorf("key %d: want %q, got %q", i, want, v)
		}
	}
}
