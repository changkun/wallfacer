package spec

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
