package spec

import (
	"slices"
	"testing"

	"changkun.de/x/wallfacer/internal/pkg/dag"
)

func TestReverseIndex_Simple(t *testing.T) {
	tree := buildTestTree(map[string]*Spec{
		"local/a.md": {Status: StatusValidated, Track: "local", DependsOn: []string{"local/b.md"}},
		"local/b.md": {Status: StatusValidated, Track: "local"},
	})
	rev := dag.ReverseEdges(Adjacency(tree))
	if !slices.Contains(rev["local/b.md"], "local/a.md") {
		t.Errorf("reverse index for b.md = %v, want [local/a.md]", rev["local/b.md"])
	}
}

func TestReverseIndex_Multiple(t *testing.T) {
	tree := buildTestTree(map[string]*Spec{
		"local/a.md": {Status: StatusValidated, Track: "local", DependsOn: []string{"local/b.md", "local/c.md"}},
		"local/b.md": {Status: StatusValidated, Track: "local"},
		"local/c.md": {Status: StatusValidated, Track: "local"},
	})
	rev := dag.ReverseEdges(Adjacency(tree))
	if !slices.Contains(rev["local/b.md"], "local/a.md") {
		t.Error("b.md should have a.md as dependent")
	}
	if !slices.Contains(rev["local/c.md"], "local/a.md") {
		t.Error("c.md should have a.md as dependent")
	}
}

func TestReverseIndex_SharedDep(t *testing.T) {
	tree := buildTestTree(map[string]*Spec{
		"local/a.md": {Status: StatusValidated, Track: "local", DependsOn: []string{"local/c.md"}},
		"local/b.md": {Status: StatusValidated, Track: "local", DependsOn: []string{"local/c.md"}},
		"local/c.md": {Status: StatusValidated, Track: "local"},
	})
	rev := dag.ReverseEdges(Adjacency(tree))
	deps := rev["local/c.md"]
	if len(deps) != 2 {
		t.Fatalf("c.md dependents = %v, want 2 entries", deps)
	}
}

func TestReverseIndex_NoDeps(t *testing.T) {
	tree := buildTestTree(map[string]*Spec{
		"local/a.md": {Status: StatusValidated, Track: "local"},
	})
	rev := dag.ReverseEdges(Adjacency(tree))
	if len(rev["local/a.md"]) != 0 {
		t.Errorf("a.md should have no dependents, got %v", rev["local/a.md"])
	}
}

func TestComputeImpact_DirectOnly(t *testing.T) {
	tree := buildTestTree(map[string]*Spec{
		"local/a.md": {Status: StatusValidated, Track: "local", DependsOn: []string{"local/b.md"}},
		"local/b.md": {Status: StatusValidated, Track: "local"},
	})
	impact := ComputeImpact(tree, "local/b.md")
	if len(impact.Direct) != 1 || impact.Direct[0] != "local/a.md" {
		t.Errorf("Direct = %v, want [local/a.md]", impact.Direct)
	}
	if len(impact.Transitive) != 0 {
		t.Errorf("Transitive = %v, want empty", impact.Transitive)
	}
}

func TestComputeImpact_Transitive(t *testing.T) {
	// A -> B -> C. Impact of C: direct=[B], transitive=[A].
	tree := buildTestTree(map[string]*Spec{
		"local/a.md": {Status: StatusValidated, Track: "local", DependsOn: []string{"local/b.md"}},
		"local/b.md": {Status: StatusValidated, Track: "local", DependsOn: []string{"local/c.md"}},
		"local/c.md": {Status: StatusValidated, Track: "local"},
	})
	impact := ComputeImpact(tree, "local/c.md")
	if !slices.Contains(impact.Direct, "local/b.md") {
		t.Errorf("Direct should contain b.md, got %v", impact.Direct)
	}
	if !slices.Contains(impact.Transitive, "local/a.md") {
		t.Errorf("Transitive should contain a.md, got %v", impact.Transitive)
	}
}

func TestComputeImpact_Diamond(t *testing.T) {
	// A -> C, B -> C, D -> A, D -> B. Impact of C: direct=[A, B], transitive=[D].
	tree := buildTestTree(map[string]*Spec{
		"local/a.md": {Status: StatusValidated, Track: "local", DependsOn: []string{"local/c.md"}},
		"local/b.md": {Status: StatusValidated, Track: "local", DependsOn: []string{"local/c.md"}},
		"local/c.md": {Status: StatusValidated, Track: "local"},
		"local/d.md": {Status: StatusValidated, Track: "local", DependsOn: []string{"local/a.md", "local/b.md"}},
	})
	impact := ComputeImpact(tree, "local/c.md")
	if len(impact.Direct) != 2 {
		t.Errorf("Direct = %v, want 2 entries", impact.Direct)
	}
	if !slices.Contains(impact.Transitive, "local/d.md") {
		t.Errorf("Transitive should contain d.md, got %v", impact.Transitive)
	}
}

func TestComputeImpact_CrossTree(t *testing.T) {
	tree := buildTestTree(map[string]*Spec{
		"cloud/x.md":  {Status: StatusValidated, Track: "cloud", DependsOn: []string{"local/y.md"}},
		"local/y.md":  {Status: StatusValidated, Track: "local"},
	})
	impact := ComputeImpact(tree, "local/y.md")
	if !slices.Contains(impact.Direct, "cloud/x.md") {
		t.Errorf("Direct should contain cloud/x.md, got %v", impact.Direct)
	}
}

func TestComputeImpact_NonLeaf(t *testing.T) {
	// parent.md has child.md. ext.md depends on child.md.
	// Impact of parent.md should include ext.md (via child).
	tree := buildTestTree(map[string]*Spec{
		"local/parent.md":       {Status: StatusValidated, Track: "local"},
		"local/parent/child.md": {Status: StatusValidated, Track: "local"},
		"local/ext.md":          {Status: StatusValidated, Track: "local", DependsOn: []string{"local/parent/child.md"}},
	})
	impact := ComputeImpact(tree, "local/parent.md")
	if !slices.Contains(impact.Direct, "local/ext.md") {
		t.Errorf("Non-leaf impact should include ext.md via child, got Direct=%v", impact.Direct)
	}
}

func TestComputeImpact_NoImpact(t *testing.T) {
	tree := buildTestTree(map[string]*Spec{
		"local/lonely.md": {Status: StatusValidated, Track: "local"},
	})
	impact := ComputeImpact(tree, "local/lonely.md")
	if len(impact.Direct) != 0 || len(impact.Transitive) != 0 {
		t.Errorf("expected empty impact, got Direct=%v Transitive=%v", impact.Direct, impact.Transitive)
	}
}

func TestComputeImpact_MissingTarget(t *testing.T) {
	tree := buildTestTree(map[string]*Spec{
		"local/a.md": {Status: StatusValidated, Track: "local", DependsOn: []string{"local/nonexistent.md"}},
	})
	// Should not panic.
	impact := ComputeImpact(tree, "local/nonexistent.md")
	if !slices.Contains(impact.Direct, "local/a.md") {
		t.Errorf("Direct should contain a.md even for missing target, got %v", impact.Direct)
	}
}

func TestUnblockedSpecs_Simple(t *testing.T) {
	tree := buildTestTree(map[string]*Spec{
		"local/a.md": {Status: StatusComplete, Track: "local"},
		"local/b.md": {Status: StatusValidated, Track: "local", DependsOn: []string{"local/a.md"}},
	})
	unblocked := UnblockedSpecs(tree, "local/a.md")
	if len(unblocked) != 1 || unblocked[0].Key != "local/b.md" {
		paths := make([]string, len(unblocked))
		for i, n := range unblocked {
			paths[i] = n.Key
		}
		t.Errorf("unblocked = %v, want [local/b.md]", paths)
	}
}

func TestUnblockedSpecs_MultiDep(t *testing.T) {
	// C depends on A and B. Only A complete -> C not unblocked.
	tree := buildTestTree(map[string]*Spec{
		"local/a.md": {Status: StatusComplete, Track: "local"},
		"local/b.md": {Status: StatusValidated, Track: "local"},
		"local/c.md": {Status: StatusValidated, Track: "local", DependsOn: []string{"local/a.md", "local/b.md"}},
	})
	unblocked := UnblockedSpecs(tree, "local/a.md")
	if len(unblocked) != 0 {
		t.Error("C should not be unblocked when B is not complete")
	}

	// Now make B complete too.
	tree.All["local/b.md"].Value.Status = StatusComplete
	unblocked = UnblockedSpecs(tree, "local/b.md")
	if len(unblocked) != 1 {
		t.Error("C should be unblocked when both A and B are complete")
	}
}

func TestUnblockedSpecs_AlreadyComplete(t *testing.T) {
	tree := buildTestTree(map[string]*Spec{
		"local/a.md": {Status: StatusComplete, Track: "local"},
		"local/b.md": {Status: StatusComplete, Track: "local", DependsOn: []string{"local/a.md"}},
	})
	unblocked := UnblockedSpecs(tree, "local/a.md")
	if len(unblocked) != 0 {
		t.Error("already-complete specs should not be returned as unblocked")
	}
}
