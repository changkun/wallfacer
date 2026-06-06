package handler

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFindSpecFile_RejectsWorkspaceEscape verifies that findSpecFile resolves
// in-tree spec paths but rejects relPaths that escape the workspace via "..".
// Before the fix, an escaped relPath resolved to a file outside the workspace,
// which the spec archive/unarchive/dispatch endpoints would then read or write.
func TestFindSpecFile_RejectsWorkspaceEscape(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(ws, "specs", "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	inTree := filepath.Join(ws, "specs", "local", "foo.md")
	if err := os.WriteFile(inTree, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A real file outside the workspace that a traversal would target.
	outside := filepath.Join(root, "secret.md")
	if err := os.WriteFile(outside, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := findSpecFile([]string{ws}, "specs/local/foo.md"); got != inTree {
		t.Errorf("in-tree spec: got %q, want %q", got, inTree)
	}
	if got := findSpecFile([]string{ws}, "../secret.md"); got != "" {
		t.Errorf("traversal escape should be rejected, got %q", got)
	}
	if got := findSpecFile([]string{ws}, "specs/../../secret.md"); got != "" {
		t.Errorf("nested traversal escape should be rejected, got %q", got)
	}
}
