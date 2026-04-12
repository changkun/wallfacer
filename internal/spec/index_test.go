package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeReadme creates a specs/README.md at the given workspace root
// with the supplied body. Parent directories are created as needed.
func writeReadme(t *testing.T, workspace, body string) string {
	t.Helper()
	path := filepath.Join(workspace, "specs", "README.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveIndex_NoReadme(t *testing.T) {
	idx, err := ResolveIndex(nil)
	if err != nil {
		t.Fatalf("ResolveIndex(nil): %v", err)
	}
	if idx != nil {
		t.Errorf("expected nil Index for empty workspace list, got %+v", idx)
	}

	// A workspace that has no specs/README.md returns nil, nil.
	dir := t.TempDir()
	idx, err = ResolveIndex([]string{dir})
	if err != nil {
		t.Fatalf("ResolveIndex: %v", err)
	}
	if idx != nil {
		t.Errorf("expected nil Index when README is missing, got %+v", idx)
	}
}

func TestResolveIndex_FirstMatchWins(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	writeReadme(t, a, "# Alpha\n")
	writeReadme(t, b, "# Bravo\n")

	idx, err := ResolveIndex([]string{a, b})
	if err != nil {
		t.Fatalf("ResolveIndex: %v", err)
	}
	if idx == nil {
		t.Fatal("expected non-nil Index")
	}
	if idx.Workspace != a {
		t.Errorf("workspace = %q, want first (%q)", idx.Workspace, a)
	}
	if idx.Title != "Alpha" {
		t.Errorf("title = %q, want %q (first workspace wins)", idx.Title, "Alpha")
	}
	if idx.Path != "specs/README.md" {
		t.Errorf("path = %q, want specs/README.md", idx.Path)
	}
}

func TestResolveIndex_SkipsWorkspacesWithoutReadme(t *testing.T) {
	// First workspace has no README; second does. Resolver should find
	// the second and return it.
	empty := t.TempDir()
	withReadme := t.TempDir()
	writeReadme(t, withReadme, "# Second Workspace\n")

	idx, err := ResolveIndex([]string{empty, withReadme})
	if err != nil {
		t.Fatalf("ResolveIndex: %v", err)
	}
	if idx == nil || idx.Workspace != withReadme {
		t.Errorf("expected Index rooted at %q, got %+v", withReadme, idx)
	}
}

func TestResolveIndex_TitleFromH1(t *testing.T) {
	dir := t.TempDir()
	writeReadme(t, dir, "Some preamble\n\n# My Custom Roadmap\n\nBody.\n")

	idx, err := ResolveIndex([]string{dir})
	if err != nil {
		t.Fatalf("ResolveIndex: %v", err)
	}
	if idx == nil {
		t.Fatal("expected non-nil Index")
	}
	if idx.Title != "My Custom Roadmap" {
		t.Errorf("title = %q, want %q", idx.Title, "My Custom Roadmap")
	}
}

func TestResolveIndex_TitleFallback(t *testing.T) {
	dir := t.TempDir()
	// File exists but has no H1 heading — fallback kicks in.
	writeReadme(t, dir, "No heading here.\n\nJust paragraphs.\n")

	idx, err := ResolveIndex([]string{dir})
	if err != nil {
		t.Fatalf("ResolveIndex: %v", err)
	}
	if idx == nil {
		t.Fatal("expected non-nil Index")
	}
	if idx.Title != indexFallbackTitle {
		t.Errorf("title = %q, want fallback %q", idx.Title, indexFallbackTitle)
	}
}

func TestResolveIndex_MtimeSet(t *testing.T) {
	dir := t.TempDir()
	path := writeReadme(t, dir, "# X\n")

	// Force a known mtime so the test doesn't race with the filesystem clock.
	want := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, want, want); err != nil {
		t.Skipf("Chtimes not supported: %v", err)
	}

	idx, err := ResolveIndex([]string{dir})
	if err != nil {
		t.Fatalf("ResolveIndex: %v", err)
	}
	if !idx.Modified.Equal(want) {
		t.Errorf("Modified = %v, want %v", idx.Modified, want)
	}
}

func TestResolveIndex_IgnoresSkipsCases(t *testing.T) {
	// README exists as a directory — should be ignored and fall through.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "specs", "README.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	idx, err := ResolveIndex([]string{dir})
	if err != nil {
		t.Fatalf("ResolveIndex: %v", err)
	}
	if idx != nil {
		t.Errorf("expected nil Index when README.md is a directory, got %+v", idx)
	}
}

func TestResolveIndex_TitleWithMultilineContent(t *testing.T) {
	// Body with headings at levels other than H1 — skipped, fallback wins.
	dir := t.TempDir()
	body := `#
## Second level
Paragraph.

# Real First Heading
`
	writeReadme(t, dir, body)
	idx, err := ResolveIndex([]string{dir})
	if err != nil {
		t.Fatalf("ResolveIndex: %v", err)
	}
	// First "# " is blank — we skip, then find "Real First Heading".
	if idx.Title != "Real First Heading" {
		t.Errorf("title = %q, want %q", idx.Title, "Real First Heading")
	}
}

func TestReadFirstH1_ScanLimit(t *testing.T) {
	dir := t.TempDir()
	// Put the H1 past the scan limit — should hit fallback.
	var b strings.Builder
	for range indexTitleScanMax + 5 {
		b.WriteString("filler line\n")
	}
	b.WriteString("# Too Late Heading\n")

	path := filepath.Join(dir, "specs", "README.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	title, err := readFirstH1(path, "Fallback")
	if err != nil {
		t.Fatalf("readFirstH1: %v", err)
	}
	if title != "Fallback" {
		t.Errorf("title = %q, want fallback", title)
	}
}
