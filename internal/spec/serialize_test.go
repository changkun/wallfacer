package spec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSerializeTree(t *testing.T) {
	dir := t.TempDir()
	specsDir := filepath.Join(dir, "specs")
	if err := os.MkdirAll(filepath.Join(specsDir, "local", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Parent spec.
	writeSpec(t, filepath.Join(specsDir, "local"), "foo.md", `---
title: Foo
status: validated
depends_on: []
affects: []
effort: large
created: 2026-01-01
updated: 2026-01-01
author: test
dispatched_task_id: null
---
# Foo
`)

	// Child spec (leaf).
	writeSpec(t, filepath.Join(specsDir, "local", "foo"), "bar.md", `---
title: Bar
status: complete
depends_on: []
affects: []
effort: small
created: 2026-01-01
updated: 2026-01-01
author: test
dispatched_task_id: null
---
# Bar
`)

	tree, err := BuildTree(specsDir)
	if err != nil {
		t.Fatal(err)
	}

	resp := SerializeTree(tree)

	if len(resp.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2", len(resp.Nodes))
	}

	// Find parent and child.
	var parent, child *NodeResponse
	for i := range resp.Nodes {
		if resp.Nodes[i].Path == "specs/local/foo.md" {
			parent = &resp.Nodes[i]
		}
		if resp.Nodes[i].Path == "specs/local/foo/bar.md" {
			child = &resp.Nodes[i]
		}
	}

	if parent == nil {
		t.Fatal("missing parent node specs/local/foo.md")
	}
	if child == nil {
		t.Fatal("missing child node specs/local/foo/bar.md")
	}

	// Parent should not be a leaf and should have bar as child.
	if parent.IsLeaf {
		t.Error("parent should not be a leaf")
	}
	if len(parent.Children) != 1 || parent.Children[0] != "specs/local/foo/bar.md" {
		t.Errorf("parent children = %v, want [specs/local/foo/bar.md]", parent.Children)
	}

	// Child should be a leaf.
	if !child.IsLeaf {
		t.Error("child should be a leaf")
	}
	if child.Spec.Title != "Bar" {
		t.Errorf("child title = %q, want %q", child.Spec.Title, "Bar")
	}

	// Progress for parent.
	p, ok := resp.Progress["specs/local/foo.md"]
	if !ok {
		t.Fatal("missing progress for parent")
	}
	if p.Complete != 1 || p.Total != 1 {
		t.Errorf("progress = %d/%d, want 1/1", p.Complete, p.Total)
	}
}

func TestSerializeTreeEmpty(t *testing.T) {
	dir := t.TempDir()
	specsDir := filepath.Join(dir, "specs")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	tree, err := BuildTree(specsDir)
	if err != nil {
		t.Fatal(err)
	}

	resp := SerializeTree(tree)
	if len(resp.Nodes) != 0 {
		t.Errorf("nodes = %d, want 0", len(resp.Nodes))
	}
	if len(resp.Progress) != 0 {
		t.Errorf("progress entries = %d, want 0", len(resp.Progress))
	}
}

func TestDateMarshalJSON(t *testing.T) {
	dir := t.TempDir()
	specsDir := filepath.Join(dir, "specs")
	if err := os.MkdirAll(filepath.Join(specsDir, "local"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeSpec(t, filepath.Join(specsDir, "local"), "test.md", `---
title: Test Date
status: drafted
depends_on: []
affects: []
effort: small
created: 2026-03-15
updated: 2026-03-30
author: test
dispatched_task_id: null
---
# Test
`)

	tree, err := BuildTree(specsDir)
	if err != nil {
		t.Fatal(err)
	}

	resp := SerializeTree(tree)
	if len(resp.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(resp.Nodes))
	}

	// Marshal to JSON and check date format.
	data, err := json.Marshal(resp.Nodes[0].Spec)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	if raw["created"] != "2026-03-15" {
		t.Errorf("created = %v, want %q", raw["created"], "2026-03-15")
	}
	if raw["updated"] != "2026-03-30" {
		t.Errorf("updated = %v, want %q", raw["updated"], "2026-03-30")
	}

	// Body should not be in JSON output.
	if _, ok := raw["body"]; ok {
		t.Error("body field should not appear in JSON output (json:\"-\")")
	}
}
