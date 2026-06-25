package spec

import (
	"slices"
	"testing"
)

func keysOf(nodes []*Node) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Key
	}
	return out
}

func TestSubtreeSpecs_LeavesAndNonLeaves(t *testing.T) {
	tree := buildTestTree(map[string]*Spec{
		"local/root.md":      {Status: StatusValidated},
		"local/root/a.md":    {Status: StatusValidated}, // leaf
		"local/root/b.md":    {Status: StatusDrafted},   // non-leaf
		"local/root/b/b1.md": {Status: StatusValidated}, // leaf
		"local/root/b/b2.md": {Status: StatusValidated}, // leaf
		"local/other.md":     {Status: StatusValidated}, // outside subtree
	})
	leaves, nonLeaves := SubtreeSpecs(tree, "local/root.md")
	wantLeaves := []string{"local/root/a.md", "local/root/b/b1.md", "local/root/b/b2.md"}
	if got := keysOf(leaves); !equalStrings(got, wantLeaves) {
		t.Errorf("leaves = %v, want %v", got, wantLeaves)
	}
	// non-leaves are root + b (any order); assert set membership.
	nl := keysOf(nonLeaves)
	if len(nl) != 2 || !slices.Contains(nl, "local/root.md") || !slices.Contains(nl, "local/root/b.md") {
		t.Errorf("nonLeaves = %v, want {root.md, b.md}", nl)
	}
}

func TestSubtreeSpecs_ArchivedPruned(t *testing.T) {
	tree := buildTestTree(map[string]*Spec{
		"local/root.md":        {Status: StatusValidated},
		"local/root/live.md":   {Status: StatusValidated},
		"local/root/arch.md":   {Status: StatusArchived}, // non-leaf, archived
		"local/root/arch/x.md": {Status: StatusValidated},
	})
	leaves, nonLeaves := SubtreeSpecs(tree, "local/root.md")
	if got := keysOf(leaves); !equalStrings(got, []string{"local/root/live.md"}) {
		t.Errorf("leaves = %v, want [local/root/live.md] (archived subtree pruned)", got)
	}
	for _, n := range nonLeaves {
		if n.Key == "local/root/arch.md" {
			t.Error("archived non-leaf must not appear in nonLeaves")
		}
	}
}

func TestSubtreeSpecs_RootArchivedOrMissing(t *testing.T) {
	tree := buildTestTree(map[string]*Spec{
		"local/root.md": {Status: StatusArchived},
	})
	if l, nl := SubtreeSpecs(tree, "local/root.md"); l != nil || nl != nil {
		t.Errorf("archived root should return nil, nil; got %v, %v", keysOf(l), keysOf(nl))
	}
	if l, nl := SubtreeSpecs(tree, "local/missing.md"); l != nil || nl != nil {
		t.Errorf("missing root should return nil, nil; got %v, %v", keysOf(l), keysOf(nl))
	}
}

func TestSubtreeSpecs_LeafRoot(t *testing.T) {
	tree := buildTestTree(map[string]*Spec{
		"local/solo.md": {Status: StatusValidated},
	})
	leaves, nonLeaves := SubtreeSpecs(tree, "local/solo.md")
	if got := keysOf(leaves); !equalStrings(got, []string{"local/solo.md"}) {
		t.Errorf("leaves = %v, want [local/solo.md]", got)
	}
	if len(nonLeaves) != 0 {
		t.Errorf("nonLeaves = %v, want empty", keysOf(nonLeaves))
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
