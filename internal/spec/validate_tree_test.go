package spec

import (
	"os"
	posixpath "path"
	"path/filepath"
	"slices"
	"testing"
	"time"

	gentree "changkun.de/x/wallfacer/internal/pkg/tree"
)

// buildTestTree creates a Tree from a map of path -> Spec for testing.
// Paths determine parent-child: "local/parent/child.md" is a child of
// "local/parent.md" if it exists. Insertion order is sorted so parents
// are added before children.
func buildTestTree(specs map[string]*Spec) *Tree {
	tree := &Tree{Tree: gentree.New[string, *Spec]()}

	// Sort paths so parents (shorter) come before children.
	paths := make([]string, 0, len(specs))
	for p := range specs {
		paths = append(paths, p)
	}
	slices.Sort(paths)

	for _, path := range paths {
		s := specs[path]
		s.Path = path
		s.Track = trackFromPath(path)
		dir := posixpath.Dir(path)
		parentPath := dir + ".md"
		if _, ok := tree.NodeAt(parentPath); ok && parentPath != path {
			tree.Add(path, s, &parentPath)
		} else {
			tree.Add(path, s, nil)
		}
	}
	return tree
}

func baseSpec() *Spec {
	return &Spec{
		Title:   "Test",
		Status:  StatusValidated,
		Effort:  EffortSmall,
		Created: Date{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		Updated: Date{time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
		Author:  "test",
		Body:    "# Test\n\nContent.",
	}
}

func TestValidateTree_Valid(t *testing.T) {
	a := baseSpec()
	a.Track = "local"
	b := baseSpec()
	b.Track = "local"
	b.DependsOn = []string{"local/a.md"}

	tree := buildTestTree(map[string]*Spec{
		"local/a.md": a,
		"local/b.md": b,
	})
	results := ValidateTree(tree, "")

	for _, r := range results {
		if r.Severity == SeverityError {
			t.Errorf("unexpected error: [%s] %s: %s", r.Rule, r.Path, r.Message)
		}
	}
}

func TestValidateTree_DirectCycle(t *testing.T) {
	a := baseSpec()
	a.DependsOn = []string{"local/b.md"}
	b := baseSpec()
	b.DependsOn = []string{"local/a.md"}

	tree := buildTestTree(map[string]*Spec{
		"local/a.md": a,
		"local/b.md": b,
	})
	results := ValidateTree(tree, "")

	if !hasRule(results, "dag-acyclic", SeverityError) {
		t.Error("expected dag-acyclic error for direct cycle")
	}
}

func TestValidateTree_TransitiveCycle(t *testing.T) {
	a := baseSpec()
	a.DependsOn = []string{"local/b.md"}
	b := baseSpec()
	b.DependsOn = []string{"local/c.md"}
	c := baseSpec()
	c.DependsOn = []string{"local/a.md"}

	tree := buildTestTree(map[string]*Spec{
		"local/a.md": a,
		"local/b.md": b,
		"local/c.md": c,
	})
	results := ValidateTree(tree, "")

	if !hasRule(results, "dag-acyclic", SeverityError) {
		t.Error("expected dag-acyclic error for transitive cycle")
	}
}

func TestValidateTree_NoCycle(t *testing.T) {
	// Diamond: A -> B, A -> C, B -> D, C -> D
	a := baseSpec()
	a.DependsOn = []string{"local/b.md", "local/c.md"}
	b := baseSpec()
	b.DependsOn = []string{"local/d.md"}
	c := baseSpec()
	c.DependsOn = []string{"local/d.md"}
	d := baseSpec()

	tree := buildTestTree(map[string]*Spec{
		"local/a.md": a,
		"local/b.md": b,
		"local/c.md": c,
		"local/d.md": d,
	})
	results := ValidateTree(tree, "")

	if hasRule(results, "dag-acyclic", SeverityError) {
		t.Error("diamond dependency is not a cycle")
	}
}

func TestValidateTree_OrphanDirectory(t *testing.T) {
	repoRoot := t.TempDir()
	specsDir := filepath.Join(repoRoot, "specs")

	// Create child spec in orphan directory (no parent .md).
	writeTestSpec(t, specsDir, "local/orphan/child.md", makeSpec("Child", "local"))

	tree, err := BuildTree(specsDir)
	if err != nil {
		t.Fatal(err)
	}
	results := ValidateTree(tree, repoRoot)

	if !hasRule(results, "no-orphan-directories", SeverityWarning) {
		t.Error("expected no-orphan-directories warning")
	}
}

func TestValidateTree_OrphanSpec(t *testing.T) {
	repoRoot := t.TempDir()
	specsDir := filepath.Join(repoRoot, "specs")

	// Create spec with empty subdirectory — should NOT trigger orphan warning.
	writeTestSpec(t, specsDir, "local/thing.md", makeSpec("Thing", "local"))
	if err := os.MkdirAll(filepath.Join(specsDir, "local", "thing"), 0755); err != nil {
		t.Fatal(err)
	}

	tree, err := BuildTree(specsDir)
	if err != nil {
		t.Fatal(err)
	}
	results := ValidateTree(tree, repoRoot)

	// No orphan warnings for this case.
	if hasRule(results, "no-orphan-directories", SeverityWarning) {
		t.Error("empty subdirectory should not trigger orphan warning")
	}
}

func TestValidateTree_StatusConsistency(t *testing.T) {
	parent := baseSpec()
	parent.Status = StatusComplete
	child := baseSpec()
	child.Status = StatusValidated // incomplete

	tree := buildTestTree(map[string]*Spec{
		"local/parent.md":       parent,
		"local/parent/child.md": child,
	})
	results := ValidateTree(tree, "")

	if !hasRule(results, "status-consistency", SeverityWarning) {
		t.Error("expected status-consistency warning for complete parent with incomplete child")
	}
}

func TestValidateTree_StalePropagate(t *testing.T) {
	dep := baseSpec()
	dep.Status = StatusStale
	dependent := baseSpec()
	dependent.Status = StatusValidated
	dependent.DependsOn = []string{"local/dep.md"}

	tree := buildTestTree(map[string]*Spec{
		"local/dep.md":       dep,
		"local/dependent.md": dependent,
	})
	results := ValidateTree(tree, "")

	if !hasRule(results, "stale-propagation", SeverityWarning) {
		t.Error("expected stale-propagation warning")
	}
}

func TestValidateTree_UniqueDispatches(t *testing.T) {
	id := "550e8400-e29b-41d4-a716-446655440000"
	a := baseSpec()
	a.DispatchedTaskID = &id
	b := baseSpec()
	b.DispatchedTaskID = &id

	tree := buildTestTree(map[string]*Spec{
		"local/a.md": a,
		"local/b.md": b,
	})
	results := ValidateTree(tree, "")

	if !hasRule(results, "unique-dispatches", SeverityError) {
		t.Error("expected unique-dispatches error")
	}
}

func TestValidateTree_NullDispatchesOK(t *testing.T) {
	a := baseSpec()
	b := baseSpec()
	// Both have nil DispatchedTaskID.

	tree := buildTestTree(map[string]*Spec{
		"local/a.md": a,
		"local/b.md": b,
	})
	results := ValidateTree(tree, "")

	if hasRule(results, "unique-dispatches", SeverityError) {
		t.Error("null dispatches should not trigger error")
	}
}

func TestValidateTree_ArchivedNonLeafIncompleteChildren(t *testing.T) {
	parent := baseSpec()
	parent.Status = StatusArchived
	child := baseSpec()
	child.Status = StatusValidated // incomplete by live-graph reckoning

	tree := buildTestTree(map[string]*Spec{
		"local/parent.md":       parent,
		"local/parent/child.md": child,
	})
	results := ValidateTree(tree, "")

	if hasRule(results, "status-consistency", SeverityWarning) {
		t.Error("archived non-leaf should be exempt from status-consistency check")
	}
}

func TestValidateTree_ArchivedDependencyNoStalePropagation(t *testing.T) {
	dep := baseSpec()
	dep.Status = StatusStale
	dependent := baseSpec()
	dependent.Status = StatusArchived
	dependent.DependsOn = []string{"local/dep.md"}

	tree := buildTestTree(map[string]*Spec{
		"local/dep.md":       dep,
		"local/dependent.md": dependent,
	})
	results := ValidateTree(tree, "")

	if hasRule(results, "stale-propagation", SeverityWarning) {
		t.Error("archived dependent should not trigger stale-propagation")
	}
}

func TestValidateTree_LiveSpecDependsOnArchived(t *testing.T) {
	archived := baseSpec()
	archived.Status = StatusArchived
	live := baseSpec()
	live.Status = StatusValidated
	live.DependsOn = []string{"local/archived.md"}

	tree := buildTestTree(map[string]*Spec{
		"local/archived.md": archived,
		"local/live.md":     live,
	})
	results := ValidateTree(tree, "")

	if !hasRule(results, "dependency-is-archived", SeverityWarning) {
		t.Error("expected dependency-is-archived warning for live spec depending on archived")
	}
	if hasRule(results, "dependency-is-archived", SeverityError) {
		t.Error("dependency-is-archived should be warning, not error")
	}
}

func TestValidateTree_ArchivedSpecDependsOnMissing(t *testing.T) {
	repoRoot := t.TempDir()
	s := baseSpec()
	s.Status = StatusArchived
	s.DependsOn = []string{"specs/nonexistent.md"}

	tree := buildTestTree(map[string]*Spec{
		"local/archived.md": s,
	})
	results := ValidateTree(tree, repoRoot)

	if !hasRule(results, "depends-on-exist", SeverityError) {
		t.Error("structural depends-on-exist should still fire for archived specs")
	}
}

func TestValidateTree_IncludesPerSpecErrors(t *testing.T) {
	s := baseSpec()
	s.Title = "" // per-spec required-fields error

	tree := buildTestTree(map[string]*Spec{
		"local/test.md": s,
	})
	results := ValidateTree(tree, "")

	if !hasRule(results, "required-fields", SeverityError) {
		t.Error("expected per-spec required-fields error in tree validation")
	}
}

func TestValidateTree_EmptyTree(t *testing.T) {
	tree := &Tree{Tree: gentree.New[string, *Spec]()}
	results := ValidateTree(tree, "")
	if len(results) != 0 {
		t.Errorf("expected no results for empty tree, got %d", len(results))
	}
}
