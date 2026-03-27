package dagscorer

import "testing"

// TestScore_SingleNode verifies that a leaf node with no children scores 1.
func TestScore_SingleNode(t *testing.T) {
	adj := map[string][]string{"a": {}}
	s := Score("a", func(n string) []string { return adj[n] })
	if s != 1 {
		t.Fatalf("expected 1, got %d", s)
	}
}

// TestScore_LinearChain verifies that a chain a->b->c scores 3 (the full path length).
func TestScore_LinearChain(t *testing.T) {
	// a -> b -> c
	adj := map[string][]string{"a": {"b"}, "b": {"c"}, "c": {}}
	s := Score("a", func(n string) []string { return adj[n] })
	if s != 3 {
		t.Fatalf("expected 3, got %d", s)
	}
}

// TestScore_DiamondDAG verifies the score through a diamond-shaped DAG where
// multiple paths converge on the same node; the result is the longest path (3).
func TestScore_DiamondDAG(t *testing.T) {
	// a -> b, a -> c, b -> d, c -> d
	adj := map[string][]string{"a": {"b", "c"}, "b": {"d"}, "c": {"d"}, "d": {}}
	s := Score("a", func(n string) []string { return adj[n] })
	if s != 3 {
		t.Fatalf("expected 3, got %d", s)
	}
}

// TestScore_Cycle verifies that cycles are broken gracefully: the cycle-back
// node returns 1, preventing infinite recursion.
func TestScore_Cycle(t *testing.T) {
	// a -> b -> a (cycle)
	adj := map[string][]string{"a": {"b"}, "b": {"a"}}
	s := Score("a", func(n string) []string { return adj[n] })
	// a -> b -> (cycle, returns 1) => b=2, a=3? No: a->b, b->a(visiting)=1, so b=1+1=2, a=1+2=3
	if s != 3 {
		t.Fatalf("expected 3, got %d", s)
	}
}

// TestScore_UnknownNode verifies that a node not in the adjacency map scores 1
// (treated as a leaf with no children).
func TestScore_UnknownNode(t *testing.T) {
	adj := map[string][]string{}
	s := Score("unknown", func(n string) []string { return adj[n] })
	// Unknown node has no children, so score = 1.
	if s != 1 {
		t.Fatalf("expected 1, got %d", s)
	}
}

// TestScore_BranchingDAG verifies that when branches have different lengths,
// the score reflects the longest path (a->b->d = 3, not a->c = 2).
func TestScore_BranchingDAG(t *testing.T) {
	// a -> b -> d (length 3)
	// a -> c (length 2)
	adj := map[string][]string{"a": {"b", "c"}, "b": {"d"}, "c": {}, "d": {}}
	s := Score("a", func(n string) []string { return adj[n] })
	if s != 3 {
		t.Fatalf("expected 3 (longest path a->b->d), got %d", s)
	}
}
