package handler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"latere.ai/x/wallfacer/internal/spec"
	"latere.ai/x/wallfacer/internal/speccomment"
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

// TestRepositionThreadMultiLineNotOrphaned reproduces the user-facing bug: a
// multi-line comment created on a real spec must reattach INLINE on the next
// load, not land in triage as orphaned. It drives the exact instance-side path
// (specFilePath -> ParseBytes -> ComputeAnchor on create, then repositionThread
// on GET) with the "specs/"-prefixed path the frontend actually sends.
func TestRepositionThreadMultiLineNotOrphaned(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\ntitle: T\n---\n\n# Heading\n\nFirst line of a paragraph.\nSecond line continues.\nThird line ends it.\n"
	if err := os.WriteFile(filepath.Join(root, "specs", "x.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create: compute a multi-line anchor exactly as SubmitSpecComment does.
	parsed, err := spec.ParseBytes([]byte(content), "x.md")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	bodyLines := strings.Split(parsed.Body, "\n")
	start, end := 0, 0
	for i, l := range bodyLines {
		if strings.HasPrefix(l, "First line") {
			start = i + 1
		}
		if strings.HasPrefix(l, "Third line") {
			end = i + 1
		}
	}
	if start == 0 || end <= start {
		t.Fatalf("fixture lines not found: start=%d end=%d", start, end)
	}
	anchor := spec.ComputeAnchor(parsed.Body, start, end)

	// GET: the frontend sends spec_path WITH the leading specs/ prefix.
	thread := speccomment.Thread{SpecPath: "specs/x.md", Anchor: anchor, Status: speccomment.StatusActive}
	got := repositionThread(thread, root)
	if got.Orphaned {
		t.Fatal("multi-line comment orphaned on display (the triage bug)")
	}
	if got.Line != start {
		t.Fatalf("reattached to line %d, want the range start %d", got.Line, start)
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
