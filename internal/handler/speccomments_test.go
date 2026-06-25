package handler

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestSpecFilePath verifies the path resolution tolerates both conventions: the
// frontend's focusedSpecPath carries the leading "specs/" while the spec-tree
// node path omits it. A mismatch here is what made every comment POST 400.
func TestSpecFilePath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "specs", "cloud"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "specs", "cloud", "x.md")
	if err := os.WriteFile(want, []byte("# X\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The frontend sends the path WITH the specs/ prefix.
	if got, ok := specFilePath(root, "specs/cloud/x.md"); !ok || got != want {
		t.Fatalf("prefixed path: got %q ok=%v, want %q true", got, ok, want)
	}
	// The spec-tree node path omits it.
	if got, ok := specFilePath(root, "cloud/x.md"); !ok || got != want {
		t.Fatalf("bare path: got %q ok=%v, want %q true", got, ok, want)
	}
	// A path that does not exist resolves to nothing (not a false 400/match).
	if _, ok := specFilePath(root, "cloud/missing.md"); ok {
		t.Fatal("nonexistent spec should not resolve")
	}
	// A directory is not a spec file.
	if _, ok := specFilePath(root, "specs/cloud"); ok {
		t.Fatal("a directory should not resolve as a spec file")
	}
}

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
