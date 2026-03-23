// Package tail provides a generic function for retaining the last N elements
// of a slice.
//
// When storing bounded history (prompt edit history, retry records), the system
// needs to trim older entries while keeping the most recent ones. [Of] returns
// a sub-slice of the last n elements, or the entire slice if it has fewer than
// n elements. This avoids repeated bounds-checking boilerplate.
//
// # Connected packages
//
// No dependencies (not even stdlib). Consumed by [store] for trimming task
// history fields (prompt history, retry records) to their configured maximums.
//
// # Usage
//
//	recent := tail.Of(allRecords, 10) // last 10 records
package tail
