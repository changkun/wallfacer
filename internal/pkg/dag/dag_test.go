package dag

import (
	"slices"
	"testing"
)

func TestReverseEdges_Simple(t *testing.T) {
	adj := map[string][]string{
		"a": {"b"},
		"b": {},
	}
	rev := ReverseEdges(adj)
	if !slices.Contains(rev["b"], "a") {
		t.Errorf("rev[b] = %v, want [a]", rev["b"])
	}
	if len(rev["a"]) != 0 {
		t.Errorf("rev[a] = %v, want []", rev["a"])
	}
}

func TestReverseEdges_Multiple(t *testing.T) {
	adj := map[string][]string{
		"a": {"c"},
		"b": {"c"},
		"c": {},
	}
	rev := ReverseEdges(adj)
	if len(rev["c"]) != 2 {
		t.Errorf("rev[c] = %v, want 2 entries", rev["c"])
	}
}

func TestReverseEdges_Empty(t *testing.T) {
	rev := ReverseEdges(map[string][]string{})
	if len(rev) != 0 {
		t.Errorf("expected empty, got %v", rev)
	}
}

func TestDetectCycles_NoCycle(t *testing.T) {
	adj := map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {},
	}
	cycles := DetectCycles(adj)
	if len(cycles) != 0 {
		t.Errorf("expected no cycles, got %v", cycles)
	}
}

func TestDetectCycles_Diamond(t *testing.T) {
	adj := map[string][]string{
		"a": {"b", "c"},
		"b": {"d"},
		"c": {"d"},
		"d": {},
	}
	cycles := DetectCycles(adj)
	if len(cycles) != 0 {
		t.Errorf("diamond is not a cycle, got %v", cycles)
	}
}

func TestDetectCycles_DirectCycle(t *testing.T) {
	adj := map[string][]string{
		"a": {"b"},
		"b": {"a"},
	}
	cycles := DetectCycles(adj)
	if len(cycles) == 0 {
		t.Fatal("expected cycle")
	}
	// Cycle should contain both a and b.
	cycle := cycles[0]
	if !slices.Contains(cycle, "a") || !slices.Contains(cycle, "b") {
		t.Errorf("cycle %v should contain a and b", cycle)
	}
}

func TestDetectCycles_TransitiveCycle(t *testing.T) {
	adj := map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {"a"},
	}
	cycles := DetectCycles(adj)
	if len(cycles) == 0 {
		t.Fatal("expected cycle")
	}
	cycle := cycles[0]
	if len(cycle) < 3 {
		t.Errorf("cycle path too short: %v", cycle)
	}
}

func TestDetectCycles_SelfLoop(t *testing.T) {
	adj := map[string][]string{
		"a": {"a"},
	}
	cycles := DetectCycles(adj)
	if len(cycles) == 0 {
		t.Fatal("expected self-loop cycle")
	}
}

func TestReachable_Linear(t *testing.T) {
	adj := map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {},
	}
	got := Reachable(adj, "a")
	if !got["b"] || !got["c"] {
		t.Errorf("reachable from a = %v, want {b, c}", got)
	}
	if got["a"] {
		t.Error("start node should not be in result")
	}
}

func TestReachable_Diamond(t *testing.T) {
	adj := map[string][]string{
		"a": {"b", "c"},
		"b": {"d"},
		"c": {"d"},
		"d": {},
	}
	got := Reachable(adj, "a")
	if len(got) != 3 {
		t.Errorf("reachable from a = %v, want 3 nodes", got)
	}
}

func TestReachable_Isolated(t *testing.T) {
	adj := map[string][]string{
		"a": {},
		"b": {},
	}
	got := Reachable(adj, "a")
	if len(got) != 0 {
		t.Errorf("isolated node should reach nothing, got %v", got)
	}
}

func TestReachable_WithCycle(t *testing.T) {
	adj := map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {"a"},
	}
	got := Reachable(adj, "a")
	if !got["b"] || !got["c"] {
		t.Errorf("reachable from a = %v, want {b, c}", got)
	}
}

func TestReverseEdges_IntKeys(t *testing.T) {
	adj := map[int][]int{
		1: {2, 3},
		2: {3},
		3: {},
	}
	rev := ReverseEdges(adj)
	if !slices.Contains(rev[3], 1) || !slices.Contains(rev[3], 2) {
		t.Errorf("rev[3] = %v, want [1, 2]", rev[3])
	}
}
