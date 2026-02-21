package store

import (
	"context"
	"testing"
)

// bg returns a background context for use in tests.
func bg() context.Context {
	return context.Background()
}

// newTestStore creates a Store backed by a fresh temporary directory.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}
