// Package keyedmu provides a per-key mutex map.
//
// It replaces the common pattern of sync.Map + LoadOrStore(&sync.Mutex{})
// with a type-safe generic wrapper.
package keyedmu

import "sync"

// Map is a collection of mutexes keyed by K. The zero value is ready to use.
// Each key gets its own mutex, created on first access.
type Map[K comparable] struct {
	m sync.Map
}

// Lock acquires the mutex for key, creating one if it does not exist.
func (km *Map[K]) Lock(key K) {
	km.load(key).Lock()
}

// Unlock releases the mutex for key. Panics if the key has no mutex.
func (km *Map[K]) Unlock(key K) {
	km.load(key).Unlock()
}

// Get returns the mutex for key, creating one if it does not exist.
// Use this when you need the raw *sync.Mutex, e.g. for defer mu.Unlock().
func (km *Map[K]) Get(key K) *sync.Mutex {
	return km.load(key)
}

// Delete removes the mutex for key, freeing memory. The caller must ensure
// no goroutine holds or is waiting on the mutex.
func (km *Map[K]) Delete(key K) {
	km.m.Delete(key)
}

func (km *Map[K]) load(key K) *sync.Mutex {
	v, _ := km.m.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}
