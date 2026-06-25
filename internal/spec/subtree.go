package spec

import "sort"

// SubtreeSpecs returns the dispatchable leaves and the non-leaf nodes within
// the subtree rooted at root (inclusive of root). Archived nodes and their
// subtrees are pruned, as are frontmatter-less doc nodes (no lifecycle to
// move). Leaves come back sorted by path for determinism; dispatch resolves
// intra-batch dependencies by pre-assigned task ID, not by slice order, so the
// order does not affect dispatch correctness. Returns nil, nil when root is
// absent or itself archived.
func SubtreeSpecs(tree *Tree, root string) (leaves, nonLeaves []*Node) {
	node, ok := tree.All[root]
	if !ok || node.Value == nil || node.Value.Status == StatusArchived {
		return nil, nil
	}

	var walk func(n *Node)
	walk = func(n *Node) {
		if n.Value == nil || n.Value.Doc {
			return
		}
		if n.Value.Status == StatusArchived {
			return // prune archived subtrees as a unit
		}
		if n.IsLeaf {
			leaves = append(leaves, n)
			return
		}
		nonLeaves = append(nonLeaves, n)
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(node)

	sort.Slice(leaves, func(i, j int) bool { return leaves[i].Key < leaves[j].Key })
	return leaves, nonLeaves
}
