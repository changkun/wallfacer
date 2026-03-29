// Package sortedkeys provides a generic helper for iterating map keys in
// sorted order.
package sortedkeys

import (
	"cmp"
	"iter"
	"maps"
	"slices"
)

// Of returns an iterator over the keys of m in sorted order.
// The keys are collected and sorted eagerly when Of is called, so the cost
// is O(n log n) regardless of how many keys the caller consumes.
func Of[K cmp.Ordered, V any](m map[K]V) iter.Seq[K] {
	return slices.Values(slices.Sorted(maps.Keys(m)))
}

// OfMap returns an iterator over key-value pairs of m in key-sorted order.
func OfMap[K cmp.Ordered, V any](m map[K]V) iter.Seq2[K, V] {
	sorted := slices.Sorted(maps.Keys(m))
	return func(yield func(K, V) bool) {
		for _, k := range sorted {
			if !yield(k, m[k]) {
				return
			}
		}
	}
}
