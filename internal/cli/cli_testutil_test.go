// Shared test utilities for the cli package. These helpers resolve the
// repository root and provide an fs.FS suitable for BuildMux, which reads
// docs from a "docs/" subtree and serves the Vue SPA from a "frontend/dist/"
// subtree.
package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"testing/fstest"
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

// testFS returns an fs.FS rooted at the repo root, used as the docs
// filesystem for BuildMux (which reads docs/guide and docs/internals).
func testFS(t *testing.T) fs.FS {
	t.Helper()
	return os.DirFS(repoRoot(t))
}

// stubVueFS returns a minimal in-memory filesystem with a frontend/dist/
// index.html, so BuildMux mounts the Vue SPA at "/" without depending on a
// built frontend/dist (which is gitignored and absent in plain `go test`).
func stubVueFS(t *testing.T) fs.FS {
	t.Helper()
	return fstest.MapFS{
		"frontend/dist/index.html": {Data: []byte("<!doctype html><head></head><body><div id=\"app\"></div></body>")},
	}
}
