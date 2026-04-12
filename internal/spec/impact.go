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
// from the tree. Archived specs contribute no edges in either direction —
// they are invisible to the live graph.
func Adjacency(tree *Tree) map[string][]string {
	adj := make(map[string][]string, len(tree.All))
	for path, node := range tree.All {
		if node.Value == nil {
			adj[path] = nil
			continue
		}
		if node.Value.Status == StatusArchived {
			adj[path] = nil
			continue
		}
		var edges []string
		for _, dep := range node.Value.DependsOn {
			if depNode, ok := tree.All[dep]; ok && depNode.Value != nil &&
				depNode.Value.Status == StatusArchived {
				continue
			}
			edges = append(edges, dep)
		}
		adj[path] = edges
	}
	return adj
}

// ComputeImpact computes the direct and transitive dependents of a spec.
// For non-leaf specs, impact includes dependents of any leaf in the subtree.
// Archived specs have no impact — they are outside the live graph.
func ComputeImpact(tree *Tree, specPath string) *Impact {
	if node, ok := tree.All[specPath]; ok && node.Value != nil &&
		node.Value.Status == StatusArchived {
		return &Impact{}
	}

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
// Archived subtrees are pruned — they contribute no seeds.
func collectLeafPaths(node *Node, paths *[]string) {
	for _, child := range node.Children {
		if child.Value == nil {
			continue
		}
		if child.Value.Status == StatusArchived {
			continue
		}
		if child.IsLeaf {
			*paths = append(*paths, child.Key)
		} else {
			collectLeafPaths(child, paths)
		}
	}
}

// UnblockedSpecs returns specs whose depends_on are now all complete,
// given that completedPath just reached complete status. Only returns specs
// that are not themselves already complete. Archived candidates are skipped —
// archival does not unblock downstream work.
func UnblockedSpecs(tree *Tree, completedPath string) []*Node {
	reverse := dag.ReverseEdges(Adjacency(tree))
	var unblocked []*Node

	for _, dependent := range reverse[completedPath] {
		node, ok := tree.All[dependent]
		if !ok || node.Value == nil {
			continue
		}
		if node.Value.Status == StatusComplete || node.Value.Status == StatusArchived {
			continue
		}
		if allDepsComplete(tree, node) {
			unblocked = append(unblocked, node)
		}
	}
	return unblocked
}

// allDepsComplete checks whether all depends_on targets of a node are complete.
// Archived dependencies count as satisfied — they are outside the live graph.
func allDepsComplete(tree *Tree, node *Node) bool {
	for _, dep := range node.Value.DependsOn {
		depNode, ok := tree.All[dep]
		if !ok || depNode.Value == nil {
			continue // missing target — skip (validation catches this)
		}
		if depNode.Value.Status != StatusComplete &&
			depNode.Value.Status != StatusArchived {
			return false
		}
	}
	return true
}
