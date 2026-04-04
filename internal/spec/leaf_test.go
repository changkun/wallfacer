package spec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsLeafPath_NoSubdirectory(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "leaf.md")
	if err := os.WriteFile(specPath, []byte("---\ntitle: Leaf\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if !IsLeafPath(specPath) {
		t.Error("spec without subdirectory should be leaf")
	}
}

func TestIsLeafPath_EmptySubdirectory(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "empty.md")
	if err := os.WriteFile(specPath, []byte("---\ntitle: Empty\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "empty"), 0755); err != nil {
		t.Fatal(err)
	}
	if !IsLeafPath(specPath) {
		t.Error("spec with empty subdirectory should be leaf")
	}
}

func TestIsLeafPath_SubdirectoryWithChildren(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "parent.md")
	if err := os.WriteFile(specPath, []byte("---\ntitle: Parent\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	childDir := filepath.Join(dir, "parent")
	if err := os.MkdirAll(childDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(childDir, "child.md"), []byte("---\ntitle: Child\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if IsLeafPath(specPath) {
		t.Error("spec with child specs in subdirectory should be non-leaf")
	}
}

func TestIsLeafPath_SubdirectoryWithOnlyNonMd(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "misc.md")
	if err := os.WriteFile(specPath, []byte("---\ntitle: Misc\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	childDir := filepath.Join(dir, "misc")
	if err := os.MkdirAll(childDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(childDir, "notes.txt"), []byte("not a spec"), 0644); err != nil {
		t.Fatal(err)
	}
	if !IsLeafPath(specPath) {
		t.Error("spec with subdirectory containing only non-.md files should be leaf")
	}
}
