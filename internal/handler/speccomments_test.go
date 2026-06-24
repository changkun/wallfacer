package handler

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestGitObjectSHAs verifies the advisory anchor metadata: a committed spec
// yields a non-empty commit and blob, and the blob changes after an edit (the
// signal the outdated/out-of-sync banner is built on).
func TestGitObjectSHAs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	specsDir := filepath.Join(root, "specs", "cloud")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	specRel := "cloud/x.md"
	specPath := filepath.Join(root, "specs", specRel)
	if err := os.WriteFile(specPath, []byte("# X\n\nfirst line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "t"},
		{"add", "."}, {"commit", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	commit, blob := gitObjectSHAs(root, specRel)
	if commit == "" || blob == "" {
		t.Fatalf("expected non-empty commit and blob, got commit=%q blob=%q", commit, blob)
	}

	// Editing the file changes the blob hash (the outdated signal), commit stays.
	if err := os.WriteFile(specPath, []byte("# X\n\nfirst line edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commit2, blob2 := gitObjectSHAs(root, specRel)
	if blob2 == blob {
		t.Fatal("blob hash should change after an edit (outdated signal)")
	}
	if commit2 != commit {
		t.Fatal("HEAD commit should not change on an uncommitted edit")
	}
}
