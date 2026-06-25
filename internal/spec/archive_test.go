package spec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestArchivePathRoundTrip(t *testing.T) {
	cases := []struct{ logical, physical string }{
		{"specs/local/foo.md", "specs/.archive/local/foo.md"},
		{"specs/foo.md", "specs/.archive/foo.md"},
		{"specs/a/b/c.md", "specs/.archive/a/b/c.md"},
	}
	for _, tc := range cases {
		if got := ArchivePath(tc.logical); got != tc.physical {
			t.Errorf("ArchivePath(%q) = %q, want %q", tc.logical, got, tc.physical)
		}
		if got := LogicalPath(tc.physical); got != tc.logical {
			t.Errorf("LogicalPath(%q) = %q, want %q", tc.physical, got, tc.logical)
		}
		if !IsArchivedPath(tc.physical) {
			t.Errorf("IsArchivedPath(%q) = false, want true", tc.physical)
		}
		if IsArchivedPath(tc.logical) {
			t.Errorf("IsArchivedPath(%q) = true, want false", tc.logical)
		}
	}
}

func TestArchivePathIdempotentAndNoop(t *testing.T) {
	// Already archived: ArchivePath is a no-op.
	if got := ArchivePath("specs/.archive/local/foo.md"); got != "specs/.archive/local/foo.md" {
		t.Errorf("ArchivePath on archived path = %q, want unchanged", got)
	}
	// Not under specs/: returned unchanged.
	if got := ArchivePath("internal/x/foo.go"); got != "internal/x/foo.go" {
		t.Errorf("ArchivePath on non-spec = %q, want unchanged", got)
	}
	// LogicalPath on a non-archive path: unchanged.
	if got := LogicalPath("specs/local/foo.md"); got != "specs/local/foo.md" {
		t.Errorf("LogicalPath on live path = %q, want unchanged", got)
	}
}

func writeArchiveTestSpec(t *testing.T, specsDir, rel, status string) {
	t.Helper()
	full := filepath.Join(specsDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\ntitle: " + rel + "\nstatus: " + status +
		"\neffort: small\ncreated: 2026-01-01\nupdated: 2026-01-02\nauthor: test\n---\n\n# Body\n\nx\n"
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildTree_ArchivePass(t *testing.T) {
	dir := t.TempDir()
	// A live spec with a live and an archived structure beneath it.
	writeArchiveTestSpec(t, dir, "local/foo.md", "validated")       // live non-leaf
	writeArchiveTestSpec(t, dir, "local/foo/live-child.md", "drafted")
	// Archived: a standalone archived spec, and a cross-boundary archived child
	// whose parent (local/foo.md) is live.
	writeArchiveTestSpec(t, dir, ".archive/local/bar.md", "archived")
	writeArchiveTestSpec(t, dir, ".archive/local/foo/arch-child.md", "archived")

	tree, err := BuildTree(dir)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}

	// Live spec unaffected.
	if n, ok := tree.All["specs/local/foo.md"]; !ok || n.Value.Status != StatusValidated {
		t.Fatalf("live foo.md missing or wrong status: %+v", n)
	}
	// Archived standalone, at its LOGICAL key, status archived, physical recorded.
	bar, ok := tree.All["specs/local/bar.md"]
	if !ok {
		t.Fatal("archived bar.md not present at logical key specs/local/bar.md")
	}
	if bar.Value.Status != StatusArchived {
		t.Errorf("bar status = %q, want archived", bar.Value.Status)
	}
	if bar.Value.PhysicalPath != "specs/.archive/local/bar.md" {
		t.Errorf("bar PhysicalPath = %q, want specs/.archive/local/bar.md", bar.Value.PhysicalPath)
	}
	// No node keyed by the physical .archive path.
	if _, bad := tree.All["specs/.archive/local/bar.md"]; bad {
		t.Error("physical .archive path leaked as a tree key")
	}
	// Cross-boundary archived child attaches to its LIVE parent.
	child, ok := tree.All["specs/local/foo/arch-child.md"]
	if !ok {
		t.Fatal("archived cross-boundary child missing")
	}
	if child.Parent == nil || child.Parent.Key != "specs/local/foo.md" {
		t.Errorf("archived child parent = %v, want specs/local/foo.md", child.Parent)
	}
	if child.Value.Status != StatusArchived {
		t.Errorf("archived child status = %q, want archived", child.Value.Status)
	}
}
