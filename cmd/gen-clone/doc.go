// Package main implements a code generator that produces a deep-clone function
// for the [changkun.de/x/wallfacer/internal/store.Task] struct.
//
// The generator parses models.go in the store package, inspects each field of the
// Task struct, and emits a deepCloneTask function that correctly copies slices,
// maps, and pointer fields. This avoids hand-maintaining clone logic that would
// drift as the Task struct evolves.
//
// # Connected packages
//
// Reads: internal/store/models.go (Task struct definition).
// Writes: internal/store/tasks_clone_gen.go (generated deep-clone function).
// When adding or removing fields from [store.Task], re-run go generate to keep
// the clone function in sync.
//
// # Usage
//
// Invoked via go:generate from internal/store/models.go:
//
//	go generate ./internal/store/
package main
