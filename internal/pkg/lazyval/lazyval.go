// Package lazyval provides an atomic cached value with lazy loading
// and explicit invalidation.
//
// It replaces the common pattern of atomic.Int32 + "load or parse" with
// a type-safe generic wrapper.
package lazyval

import "sync"

// Value is a lazily-computed, atomically-cached value. The zero value is
// ready to use once Init is called.
type Value[T comparable] struct {
	mu    sync.Mutex
	val   T
	valid bool
	zero  T
	load  func() T
}

// New creates a Value that calls load to compute the value on first access
// or after Invalidate.
func New[T comparable](load func() T) *Value[T] {
	return &Value[T]{load: load}
}

// Get returns the cached value, calling the load function if the cache is
// invalid. Concurrent callers serialize on the mutex, so only the first
// caller invokes load; subsequent callers see the cached result.
func (v *Value[T]) Get() T {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.valid {
		v.val = v.load()
		v.valid = true
	}
	return v.val
}

// Invalidate clears the cached value so the next Get re-runs load.
func (v *Value[T]) Invalidate() {
	v.mu.Lock()
	v.val = v.zero
	v.valid = false
	v.mu.Unlock()
}
