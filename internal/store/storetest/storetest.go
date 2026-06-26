// Package storetest provides constructors for [store.Store] in tests that
// register cleanup to drain background trace compaction.
//
// A terminal-state task transition schedules a background compaction goroutine
// that writes the task's traces/ directory. If a test creates its store under
// t.TempDir and returns without draining that goroutine, the testing package's
// RemoveAll races the compaction write and fails with "directory not empty".
// Always construct test stores through this package so the drain (store.Close,
// which blocks new compactions then waits for in-flight ones) is registered as
// a t.Cleanup. Because it is registered after the directory it runs before the
// RemoveAll (cleanups run LIFO), closing the race.
package storetest

import (
	"testing"

	"latere.ai/x/wallfacer/internal/store"
)

// NewFileStore is a drop-in replacement for [store.NewFileStore] that also
// registers tb.Cleanup(s.Close) so background compaction is drained before the
// temp directory is removed. It returns the same (store, error) pair so call
// sites need no other change.
func NewFileStore(tb testing.TB, dir string) (*store.Store, error) {
	tb.Helper()
	s, err := store.NewFileStore(dir)
	if err == nil {
		tb.Cleanup(s.Close)
	}
	return s, err
}

// NewStore is a drop-in replacement for [store.NewStore] with the same cleanup
// guarantee as [NewFileStore]. Use it for stores backed by a custom
// StorageBackend.
func NewStore(tb testing.TB, backend store.StorageBackend) (*store.Store, error) {
	tb.Helper()
	s, err := store.NewStore(backend)
	if err == nil {
		tb.Cleanup(s.Close)
	}
	return s, err
}
