package spec

import (
	"changkun.de/x/wallfacer/internal/pkg/dag"
)

// Impact describes the direct and transitive dependents of a spec.
type Impact struct {
	Direct     []string // spec paths that directly depend on the target
	Transitive []string // spec paths reachable transitively (excluding direct)
}

// Adjacency builds a forward adjacency map (spec path → its depends_on targets)
// from the tree. This is the input format for the dag package operations.
func Adjacency(tree *Tree) map[string][]string {
	adj := make(map[string][]string, len(tree.All))
	for path, node := range tree.All {
		if node.Spec != nil {
			adj[path] = node.Spec.DependsOn
		} else {
			adj[path] = nil
		}
	}
	return adj
}

// ComputeImpact computes the direct and transitive dependents of a spec.
// For non-leaf specs, impact includes dependents of any leaf in the subtree.
func ComputeImpact(tree *Tree, specPath string) *Impact {
	reverse := dag.ReverseEdges(Adjacency(tree))

	// Collect seed paths: the spec itself plus all subtree leaves for non-leaf specs.
	seeds := []string{specPath}
	if node, ok := tree.All[specPath]; ok && !node.IsLeaf {
		collectLeafPaths(node, &seeds)
	}

	// Gather direct dependents from all seed paths.
	directSet := make(map[string]bool)
	for _, seed := range seeds {
		for _, dep := range reverse[seed] {
			if dep != specPath {
				directSet[dep] = true
			}
		}
	}

	// BFS from direct dependents to find transitive dependents.
	transitiveSet := make(map[string]bool)
	for d := range directSet {
		for r := range dag.Reachable(reverse, d) {
			if !directSet[r] && r != specPath {
				transitiveSet[r] = true
			}
		}
	}

	impact := &Impact{
		Direct:     make([]string, 0, len(directSet)),
		Transitive: make([]string, 0, len(transitiveSet)),
	}
	for d := range directSet {
		impact.Direct = append(impact.Direct, d)
	}
	for t := range transitiveSet {
		impact.Transitive = append(impact.Transitive, t)
	}
	return impact
}

// collectLeafPaths appends all leaf spec paths in the subtree to paths.
func collectLeafPaths(node *Node, paths *[]string) {
	for _, child := range node.Children {
		if child.Spec == nil {
			continue
		}
		if child.IsLeaf {
			*paths = append(*paths, child.Spec.Path)
		} else {
			collectLeafPaths(child, paths)
		}
	}
}

// UnblockedSpecs returns specs whose depends_on are now all complete,
// given that completedPath just reached complete status. Only returns specs
// that are not themselves already complete.
func UnblockedSpecs(tree *Tree, completedPath string) []*Node {
	reverse := dag.ReverseEdges(Adjacency(tree))
	var unblocked []*Node

	for _, dependent := range reverse[completedPath] {
		node, ok := tree.All[dependent]
		if !ok || node.Spec == nil {
			continue
		}
		if node.Spec.Status == StatusComplete {
			continue
		}
		if allDepsComplete(tree, node) {
			unblocked = append(unblocked, node)
		}
	}
	return unblocked
}

// allDepsComplete checks whether all depends_on targets of a node are complete.
func allDepsComplete(tree *Tree, node *Node) bool {
	for _, dep := range node.Spec.DependsOn {
		depNode, ok := tree.All[dep]
		if !ok || depNode.Spec == nil {
			continue // missing target — skip (validation catches this)
		}
		if depNode.Spec.Status != StatusComplete {
			return false
		}
	}
	return true
}
