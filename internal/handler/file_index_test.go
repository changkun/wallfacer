package handler

import (
	"path/filepath"
	"testing"
	"time"
)

// TestBuildFiles_BrokenWorkspace verifies that when workspaceMtime fails
// (non-existent directory), buildFiles returns a non-zero mtime close to
// time.Now() rather than the zero value. A zero mtime would cause the cache
// freshness check (entry.rootMTime.After(entry.builtAt)) to permanently return
// false, silently freezing the file list.
func TestBuildFiles_BrokenWorkspace(t *testing.T) {
	// Use a path guaranteed not to exist so os.Stat returns an error.
	ws := filepath.Join(t.TempDir(), "nonexistent")

	before := time.Now()
	_, mt := buildFiles(ws)

	if mt.IsZero() {
		t.Fatal("mtime must not be zero when workspaceMtime fails; got zero value (year 0001)")
	}
	if mt.Before(before) {
		t.Errorf("mtime %v predates test start %v; expected it to be close to time.Now()", mt, before)
	}
}
