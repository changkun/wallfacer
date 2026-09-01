package handler

import "sync"

// lazy is a single value computed on first read and again after Invalidate.
// It is what the parallelism caps need: the env file is parsed once, not on
// every task launch, and re-parsed only after the settings handler writes it.
type lazy[T any] struct {
	mu    sync.Mutex
	val   T
	valid bool
	load  func() T
}

func newLazy[T any](load func() T) *lazy[T] { return &lazy[T]{load: load} }

// Get returns the value, computing it under the lock when it is not held.
func (l *lazy[T]) Get() T {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.valid {
		l.val = l.load()
		l.valid = true
	}
	return l.val
}

// Invalidate drops the value so the next Get recomputes it.
func (l *lazy[T]) Invalidate() {
	var zero T
	l.mu.Lock()
	l.val = zero
	l.valid = false
	l.mu.Unlock()
}
