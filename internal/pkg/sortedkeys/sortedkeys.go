// Package sortedkeys provides a generic helper for iterating map keys in
// sorted order.
package sortedkeys

import (
	"cmp"
	"iter"
	"slices"
)

// Of returns an iterator over the keys of m in sorted order.
func Of[K cmp.Ordered, V any](m map[K]V) iter.Seq[K] {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return func(yield func(K) bool) {
		for _, k := range keys {
			if !yield(k) {
				return
			}
		}
	}
}

// OfMap returns an iterator over key-value pairs of m in key-sorted order.
func OfMap[K cmp.Ordered, V any](m map[K]V) iter.Seq2[K, V] {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return func(yield func(K, V) bool) {
		for _, k := range keys {
			if !yield(k, m[k]) {
				return
			}
		}
	}
}
