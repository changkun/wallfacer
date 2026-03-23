// Package sortedkeys provides generic helpers for iterating map keys in sorted
// order using Go 1.22+ iterators.
//
// Maps in Go have non-deterministic iteration order. When producing stable output
// (Prometheus metrics, container environment variables), deterministic key ordering
// is required. [Of] returns keys in sorted order, and [OfMap] returns key-value
// pairs in sorted order, both as range-compatible iterators.
//
// # Connected packages
//
// No internal dependencies (stdlib only). Consumed by [metrics] (deterministic
// label ordering in Prometheus text output) and [runner] (stable environment
// variable ordering in container specs).
//
// # Usage
//
//	for k := range sortedkeys.Of(m) {
//	    fmt.Println(k, m[k])
//	}
//	for k, v := range sortedkeys.OfMap(m) {
//	    fmt.Println(k, v)
//	}
package sortedkeys
