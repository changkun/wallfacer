// Shared test utilities for the cli package. These helpers resolve the
// repository root and provide an fs.FS suitable for BuildMux, which
// expects "ui/" and "docs/" subtrees.
package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// repoRoot returns the repository root directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// This file is at internal/cli/cli_testutil_test.go, so root is ../../
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// testFS returns an fs.FS rooted at the repo root, suitable for BuildMux
// (which expects "ui/" and "docs/" subdirectories).
func testFS(t *testing.T) fs.FS {
	t.Helper()
	return os.DirFS(repoRoot(t))
}
