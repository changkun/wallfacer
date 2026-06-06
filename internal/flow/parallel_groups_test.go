package flow

import "testing"

// TestBuildParallelGroups_OneSidedEdge verifies that a RunInParallelWith edge
// declared on only one side still groups the two steps together. Before the fix
// adjacency was directed and built in index order with a shared visited set, so
// a later step listing an earlier peer that did not list it back was placed in
// its own group and run sequentially.
func TestBuildParallelGroups_OneSidedEdge(t *testing.T) {
	// b (index 0) does not reference a; c (index 1) lists b one-sidedly.
	steps := []Step{
		{AgentSlug: "b"},
		{AgentSlug: "c", RunInParallelWith: []string{"b"}},
	}
	groups := buildParallelGroups(steps)
	if len(groups) != 1 {
		t.Fatalf("expected b and c in a single parallel group, got %d groups: %v", len(groups), groups)
	}
	if len(groups[0]) != 2 {
		t.Fatalf("expected group of 2, got %d", len(groups[0]))
	}
}

// TestBuildParallelGroups_SeparateStepsStaySeparate is the negative control:
// steps with no RunInParallelWith edges run sequentially (one group each).
func TestBuildParallelGroups_SeparateStepsStaySeparate(t *testing.T) {
	steps := []Step{{AgentSlug: "a"}, {AgentSlug: "b"}}
	groups := buildParallelGroups(steps)
	if len(groups) != 2 {
		t.Fatalf("expected 2 separate groups, got %d: %v", len(groups), groups)
	}
}
