package spec

import (
	"slices"
	"testing"

	"latere.ai/x/pkg/dag"
)

func TestReverseIndex_Simple(t *testing.T) {
	tree := buildTestTree(map[string]*Spec{
		"local/a.md": {Status: StatusValidated, DependsOn: []string{"local/b.md"}},
		"local/b.md": {Status: StatusValidated},
	})
	rev := dag.ReverseEdges(Adjacency(tree))
	if !slices.Contains(rev["local/b.md"], "local/a.md") {
		t.Errorf("reverse index for b.md = %v, want [local/a.md]", rev["local/b.md"])
	}
}

func TestAdjacency_SkipsArchived(t *testing.T) {
	tree := buildTestTree(map[string]*Spec{
		"local/archived.md": {Status: StatusArchived, DependsOn: []string{"local/x.md"}},
		"local/live.md":     {Status: StatusValidated, DependsOn: []string{"local/archived.md", "local/x.md"}},
		"local/x.md":        {Status: StatusValidated},
	})
	adj := Adjacency(tree)
	if len(adj["local/archived.md"]) != 0 {
		t.Errorf("archived source should have no outgoing edges, got %v", adj["local/archived.md"])
	}
	if slices.Contains(adj["local/live.md"], "local/archived.md") {
		t.Errorf("edge to archived sink should be excluded, got %v", adj["local/live.md"])
	}
	if !slices.Contains(adj["local/live.md"], "local/x.md") {
		t.Errorf("non-archived edge should remain, got %v", adj["local/live.md"])
	}
}
