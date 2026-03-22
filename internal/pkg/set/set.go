// Package set provides a generic set type backed by a map.
package set

// Set is an unordered collection of unique elements.
// The zero value is not usable; create sets with [New] or [From].
type Set[T comparable] struct {
	m map[T]struct{}
}

// New creates a Set containing the given items.
func New[T comparable](items ...T) Set[T] {
	s := Set[T]{m: make(map[T]struct{}, len(items))}
	for _, item := range items {
		s.m[item] = struct{}{}
	}
	return s
}

// From creates a Set from a slice.
func From[T comparable](slice []T) Set[T] {
	return New(slice...)
}

// Add inserts an item into the set.
func (s *Set[T]) Add(item T) {
	s.m[item] = struct{}{}
}

// Remove deletes an item from the set.
func (s *Set[T]) Remove(item T) {
	delete(s.m, item)
}

// Has reports whether item is in the set.
func (s *Set[T]) Has(item T) bool {
	_, ok := s.m[item]
	return ok
}

// Len returns the number of elements.
func (s *Set[T]) Len() int {
	return len(s.m)
}

// Items returns all elements in arbitrary order.
func (s *Set[T]) Items() []T {
	result := make([]T, 0, len(s.m))
	for item := range s.m {
		result = append(result, item)
	}
	return result
}
