package spec

import (
	"os"
	"path/filepath"
	"testing"
)

// makeSpec returns minimal valid spec frontmatter with the given title and track.
func makeSpec(title, track string) string {
	return "---\ntitle: " + title + "\nstatus: drafted\ntrack: " + track +
		"\neffort: small\ncreated: 2026-01-01\nupdated: 2026-01-01\nauthor: test\n" +
		"dispatched_task_id: null\n---\n"
}

// writeTestSpec writes a spec file into the test directory, creating parent dirs.
func writeTestSpec(t *testing.T, specsDir, relPath, content string) {
	t.Helper()
	full := filepath.Join(specsDir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestBuildTree_SingleSpec(t *testing.T) {
	specsDir := filepath.Join(t.TempDir(), "specs")
	writeTestSpec(t, specsDir, "local/solo.md", makeSpec("Solo", "local"))

	tree, err := BuildTree(specsDir)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}
	if len(tree.Errs) != 0 {
		t.Fatalf("unexpected errors: %v", tree.Errs)
	}
	if len(tree.Roots) != 1 {
		t.Fatalf("Roots = %d, want 1", len(tree.Roots))
	}
	root := tree.Roots[0]
	if !root.IsLeaf {
		t.Error("expected leaf")
	}
	if root.Depth != 0 {
		t.Errorf("Depth = %d, want 0", root.Depth)
	}
	if root.Value.Title != "Solo" {
		t.Errorf("Title = %q, want %q", root.Value.Title, "Solo")
	}
}

func TestBuildTree_ParentWithChildren(t *testing.T) {
	specsDir := filepath.Join(t.TempDir(), "specs")
	writeTestSpec(t, specsDir, "local/foo.md", makeSpec("Foo", "local"))
	writeTestSpec(t, specsDir, "local/foo/bar.md", makeSpec("Bar", "local"))
	writeTestSpec(t, specsDir, "local/foo/baz.md", makeSpec("Baz", "local"))

	tree, err := BuildTree(specsDir)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}
	if len(tree.Errs) != 0 {
		t.Fatalf("unexpected errors: %v", tree.Errs)
	}

	parent, ok := tree.NodeAt("specs/local/foo.md")
	if !ok {
		t.Fatal("parent not found")
	}
	if parent.IsLeaf {
		t.Error("parent should not be leaf")
	}
	if len(parent.Children) != 2 {
		t.Fatalf("Children = %d, want 2", len(parent.Children))
	}
	for _, child := range parent.Children {
		if child.Parent != parent {
			t.Error("child.Parent does not point to parent")
		}
		if !child.IsLeaf {
			t.Error("child should be leaf")
		}
		if child.Depth != 1 {
			t.Errorf("child.Depth = %d, want 1", child.Depth)
		}
	}
}

func TestBuildTree_DeepNesting(t *testing.T) {
	specsDir := filepath.Join(t.TempDir(), "specs")
	writeTestSpec(t, specsDir, "foundations/a.md", makeSpec("A", "foundations"))
	writeTestSpec(t, specsDir, "foundations/a/b.md", makeSpec("B", "foundations"))
	writeTestSpec(t, specsDir, "foundations/a/b/c.md", makeSpec("C", "foundations"))

	tree, err := BuildTree(specsDir)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}

	c, ok := tree.NodeAt("specs/foundations/a/b/c.md")
	if !ok {
		t.Fatal("deep node not found")
	}
	if c.Depth != 2 {
		t.Errorf("c.Depth = %d, want 2", c.Depth)
	}
	if c.Parent == nil || c.Parent.Value.Title != "B" {
		t.Error("c.Parent should be B")
	}
	if c.Parent.Parent == nil || c.Parent.Parent.Value.Title != "A" {
		t.Error("c.Parent.Parent should be A")
	}
	if c.Parent.Parent.Parent != nil {
		t.Error("root parent should be nil")
	}
}

func TestBuildTree_MultipleTracks(t *testing.T) {
	specsDir := filepath.Join(t.TempDir(), "specs")
	writeTestSpec(t, specsDir, "foundations/x.md", makeSpec("X", "foundations"))
	writeTestSpec(t, specsDir, "local/y.md", makeSpec("Y", "local"))
	writeTestSpec(t, specsDir, "cloud/z.md", makeSpec("Z", "cloud"))

	tree, err := BuildTree(specsDir)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}

	if len(tree.ByTrack("foundations")) != 1 {
		t.Errorf("foundations roots = %d, want 1", len(tree.ByTrack("foundations")))
	}
	if len(tree.ByTrack("local")) != 1 {
		t.Errorf("local roots = %d, want 1", len(tree.ByTrack("local")))
	}
	if len(tree.ByTrack("cloud")) != 1 {
		t.Errorf("cloud roots = %d, want 1", len(tree.ByTrack("cloud")))
	}
	if len(tree.ByTrack("shared")) != 0 {
		t.Errorf("shared roots = %d, want 0", len(tree.ByTrack("shared")))
	}
}

func TestBuildTree_LeafDetection(t *testing.T) {
	specsDir := filepath.Join(t.TempDir(), "specs")
	writeTestSpec(t, specsDir, "local/parent.md", makeSpec("Parent", "local"))
	writeTestSpec(t, specsDir, "local/parent/child.md", makeSpec("Child", "local"))
	writeTestSpec(t, specsDir, "local/standalone.md", makeSpec("Standalone", "local"))

	tree, err := BuildTree(specsDir)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}

	parent, _ := tree.NodeAt("specs/local/parent.md")
	if parent.IsLeaf {
		t.Error("parent should not be leaf")
	}

	child, _ := tree.NodeAt("specs/local/parent/child.md")
	if !child.IsLeaf {
		t.Error("child should be leaf")
	}

	standalone, _ := tree.NodeAt("specs/local/standalone.md")
	if !standalone.IsLeaf {
		t.Error("standalone should be leaf")
	}
}

func TestBuildTree_AllIndex(t *testing.T) {
	specsDir := filepath.Join(t.TempDir(), "specs")
	writeTestSpec(t, specsDir, "local/a.md", makeSpec("A", "local"))
	writeTestSpec(t, specsDir, "local/a/b.md", makeSpec("B", "local"))
	writeTestSpec(t, specsDir, "foundations/c.md", makeSpec("C", "foundations"))

	tree, err := BuildTree(specsDir)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}

	paths := []string{"specs/local/a.md", "specs/local/a/b.md", "specs/foundations/c.md"}
	for _, p := range paths {
		if _, ok := tree.NodeAt(p); !ok {
			t.Errorf("missing from All index: %s", p)
		}
	}
	if len(tree.All) != 3 {
		t.Errorf("All has %d entries, want 3", len(tree.All))
	}
}

func TestBuildTree_Leaves(t *testing.T) {
	specsDir := filepath.Join(t.TempDir(), "specs")
	writeTestSpec(t, specsDir, "local/parent.md", makeSpec("Parent", "local"))
	writeTestSpec(t, specsDir, "local/parent/a.md", makeSpec("A", "local"))
	writeTestSpec(t, specsDir, "local/parent/b.md", makeSpec("B", "local"))
	writeTestSpec(t, specsDir, "local/solo.md", makeSpec("Solo", "local"))

	tree, err := BuildTree(specsDir)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}

	count := 0
	for l := range tree.Leaves() {
		if !l.IsLeaf {
			t.Errorf("Leaves() returned non-leaf: %s", l.Key)
		}
		count++
	}
	if count != 3 {
		t.Fatalf("Leaves() = %d, want 3", count)
	}
}

func TestBuildTree_OrphanDirectory(t *testing.T) {
	specsDir := filepath.Join(t.TempDir(), "specs")
	// Create orphan directory (no matching .md file for "orphan/")
	writeTestSpec(t, specsDir, "local/orphan/child.md", makeSpec("Child", "local"))

	tree, err := BuildTree(specsDir)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}

	// Child should still be parsed and present.
	child, ok := tree.NodeAt("specs/local/orphan/child.md")
	if !ok {
		t.Fatal("orphan child not found in tree")
	}
	if child.Value.Title != "Child" {
		t.Errorf("Title = %q, want %q", child.Value.Title, "Child")
	}
}

func TestBuildTree_EmptySubdirectory(t *testing.T) {
	specsDir := filepath.Join(t.TempDir(), "specs")
	writeTestSpec(t, specsDir, "local/spec.md", makeSpec("Spec", "local"))
	// Create empty subdirectory matching the spec name.
	if err := os.MkdirAll(filepath.Join(specsDir, "local", "spec"), 0755); err != nil {
		t.Fatal(err)
	}

	tree, err := BuildTree(specsDir)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}

	node, ok := tree.NodeAt("specs/local/spec.md")
	if !ok {
		t.Fatal("spec not found")
	}
	if !node.IsLeaf {
		t.Error("spec with empty subdirectory should still be leaf")
	}
}

func makeSpecWithBody(title, track, body string) string {
	return "---\ntitle: " + title + "\nstatus: drafted\ntrack: " + track +
		"\neffort: small\ncreated: 2026-01-01\nupdated: 2026-01-01\nauthor: test\n" +
		"dispatched_task_id: null\n---\n\n" + body
}

func TestBuildTree_ChildOrderFromParentBody(t *testing.T) {
	specsDir := filepath.Join(t.TempDir(), "specs")

	// Parent body links children in order: charlie, alpha, bravo.
	parentBody := `# My Spec

## Breakdown

| # | Sub-spec |
|---|----------|
| 1 | [Charlie](parent/charlie.md) |
| 2 | [Alpha](parent/alpha.md) |
| 3 | [Bravo](parent/bravo.md) |
`
	writeTestSpec(t, specsDir, "local/parent.md", makeSpecWithBody("Parent", "local", parentBody))
	writeTestSpec(t, specsDir, "local/parent/alpha.md", makeSpec("Alpha", "local"))
	writeTestSpec(t, specsDir, "local/parent/bravo.md", makeSpec("Bravo", "local"))
	writeTestSpec(t, specsDir, "local/parent/charlie.md", makeSpec("Charlie", "local"))

	tree, err := BuildTree(specsDir)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}
	if len(tree.Errs) != 0 {
		t.Fatalf("unexpected errors: %v", tree.Errs)
	}

	parent, ok := tree.NodeAt("specs/local/parent.md")
	if !ok {
		t.Fatal("parent not found")
	}
	if len(parent.Children) != 3 {
		t.Fatalf("Children = %d, want 3", len(parent.Children))
	}

	// Without the fix, alphabetical order would be: alpha, bravo, charlie.
	// With the fix, order follows the parent body links: charlie, alpha, bravo.
	wantOrder := []string{
		"specs/local/parent/charlie.md",
		"specs/local/parent/alpha.md",
		"specs/local/parent/bravo.md",
	}
	for i, child := range parent.Children {
		if child.Key != wantOrder[i] {
			t.Errorf("Children[%d].Key = %q, want %q", i, child.Key, wantOrder[i])
		}
	}
}

func TestBuildTree_ChildOrderPartialLinks(t *testing.T) {
	specsDir := filepath.Join(t.TempDir(), "specs")

	// Parent body only links bravo; alpha and charlie should follow in alphabetical order.
	parentBody := "See [Bravo](parent/bravo.md) for details.\n"
	writeTestSpec(t, specsDir, "local/parent.md", makeSpecWithBody("Parent", "local", parentBody))
	writeTestSpec(t, specsDir, "local/parent/alpha.md", makeSpec("Alpha", "local"))
	writeTestSpec(t, specsDir, "local/parent/bravo.md", makeSpec("Bravo", "local"))
	writeTestSpec(t, specsDir, "local/parent/charlie.md", makeSpec("Charlie", "local"))

	tree, err := BuildTree(specsDir)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}

	parent, _ := tree.NodeAt("specs/local/parent.md")
	wantOrder := []string{
		"specs/local/parent/bravo.md",   // referenced first
		"specs/local/parent/alpha.md",   // unreferenced, alphabetical
		"specs/local/parent/charlie.md", // unreferenced, alphabetical
	}
	for i, child := range parent.Children {
		if child.Key != wantOrder[i] {
			t.Errorf("Children[%d].Key = %q, want %q", i, child.Key, wantOrder[i])
		}
	}
}

func TestBuildTree_EmptySpecsDir(t *testing.T) {
	specsDir := filepath.Join(t.TempDir(), "specs")
	if err := os.MkdirAll(specsDir, 0755); err != nil {
		t.Fatal(err)
	}

	tree, err := BuildTree(specsDir)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}
	if len(tree.Roots) != 0 {
		t.Errorf("Roots = %d, want 0", len(tree.Roots))
	}
	if len(tree.All) != 0 {
		t.Errorf("All = %d, want 0", len(tree.All))
	}
}

func TestBuildTree_NonexistentDir(t *testing.T) {
	tree, err := BuildTree(filepath.Join(t.TempDir(), "nonexistent"))
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}
	if len(tree.Roots) != 0 {
		t.Errorf("Roots = %d, want 0", len(tree.Roots))
	}
}
