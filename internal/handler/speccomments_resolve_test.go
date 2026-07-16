package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestResolveSpecRepo_PrefersRemoteBearingFolder proves that when several
// visible workspace folders contain the same spec, the folder that has a git
// remote is chosen even when a remote-less folder is listed first. Matching the
// first folder blindly returns an empty repo and rejects every spec-comment op
// with "spec workspace has no git remote".
func TestResolveSpecRepo_PrefersRemoteBearingFolder(t *testing.T) {
	git := func(dir string, args ...string) {
		if err := exec.Command("git", append([]string{"-C", dir}, args...)...).Run(); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	mkSpec := func() string {
		d := t.TempDir()
		if err := os.MkdirAll(filepath.Join(d, "specs"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "specs", "x.md"), []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return d
	}

	noRemote := mkSpec()
	git(noRemote, "init")
	withRemote := mkSpec()
	git(withRemote, "init")
	git(withRemote, "remote", "add", "origin", "git@github.com:acme/repo.git")

	// noRemote is listed FIRST; the remote-bearing folder must still win.
	h := &Handler{workspaces: []string{noRemote, withRemote}}
	req := httptest.NewRequest(http.MethodGet, "/api/spec-comments", nil)

	repo, root, ok := h.resolveSpecRepo(req, "specs/x.md")
	if !ok {
		t.Fatal("resolveSpecRepo: not ok, want a match")
	}
	if repo == "" {
		t.Fatal("expected the remote-bearing folder's repo, got empty (the bug)")
	}
	if root != withRemote {
		t.Fatalf("root = %q, want the remote-bearing folder %q", root, withRemote)
	}
}
