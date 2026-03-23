package keyedmu

import (
	"sync"
	"testing"
)

func TestMap_LockUnlock(_ *testing.T) {
	var km Map[string]

	km.Lock("a")
	km.Unlock("a")
}

func TestMap_Get(t *testing.T) {
	var km Map[string]

	mu1 := km.Get("x")
	mu2 := km.Get("x")
	if mu1 != mu2 {
		t.Fatal("Get should return the same mutex for the same key")
	}

	mu3 := km.Get("y")
	if mu1 == mu3 {
		t.Fatal("Get should return different mutexes for different keys")
	}
}

func TestMap_Delete(t *testing.T) {
	var km Map[int]

	mu1 := km.Get(1)
	km.Delete(1)
	mu2 := km.Get(1)
	if mu1 == mu2 {
		t.Fatal("after Delete, Get should return a new mutex")
	}
}

func TestMap_Concurrent(t *testing.T) {
	var km Map[int]
	var wg sync.WaitGroup
	counter := make([]int, 10)

	for key := 0; key < 10; key++ {
		for g := 0; g < 100; g++ {
			wg.Add(1)
			go func(k int) {
				defer wg.Done()
				km.Lock(k)
				defer km.Unlock(k)
				counter[k]++
			}(key)
		}
	}
	wg.Wait()

	for i, c := range counter {
		if c != 100 {
			t.Errorf("key %d: expected 100, got %d", i, c)
		}
	}
}
