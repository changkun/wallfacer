// Package syncmap provides a type-safe wrapper around sync.Map.
package syncmap

import (
	"iter"
	"sync"
)

// Map is a type-safe concurrent map. The zero value is ready to use.
type Map[K comparable, V any] struct {
	m sync.Map
}

// Store sets the value for a key.
func (m *Map[K, V]) Store(key K, val V) {
	m.m.Store(key, val)
}

// Load returns the value stored under key and whether it was found.
func (m *Map[K, V]) Load(key K) (V, bool) {
	v, ok := m.m.Load(key)
	if !ok {
		var zero V
		return zero, false
	}
	return v.(V), true
}

// Delete removes the entry for key.
func (m *Map[K, V]) Delete(key K) {
	m.m.Delete(key)
}

// Range calls fn for each entry. Iteration stops if fn returns false.
func (m *Map[K, V]) Range(fn func(K, V) bool) {
	m.m.Range(func(k, v any) bool {
		return fn(k.(K), v.(V))
	})
}

// All returns an iterator over all key-value pairs.
func (m *Map[K, V]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		m.m.Range(func(k, v any) bool {
			return yield(k.(K), v.(V))
		})
	}
}
