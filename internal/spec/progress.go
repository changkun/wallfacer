package spec

import "fmt"

// Progress tracks completion of leaves in a spec subtree.
type Progress struct {
	Complete int // number of complete leaves in subtree
	Total    int // total number of leaves in subtree
}

// Fraction returns the completion ratio (0.0 if no leaves).
func (p Progress) Fraction() float64 {
	if p.Total == 0 {
		return 0
	}
	return float64(p.Complete) / float64(p.Total)
}

// String returns a human-readable progress summary.
func (p Progress) String() string {
	return fmt.Sprintf("%d/%d leaves done", p.Complete, p.Total)
}

// NodeProgress recursively computes progress for a node.
// Leaf nodes count as 1 total (complete if status is "complete").
// Non-leaf nodes sum the progress of all children.
// Archived leaves contribute 0 to both Complete and Total; an archived non-leaf
// masks its entire subtree from progress.
func NodeProgress(node *Node) Progress {
	if node.Value != nil && node.Value.Status == StatusArchived {
		return Progress{}
	}
	if node.IsLeaf {
		if node.Value == nil {
			return Progress{}
		}
		if node.Value.Status == StatusComplete {
			return Progress{Complete: 1, Total: 1}
		}
		return Progress{Complete: 0, Total: 1}
	}
	var p Progress
	for _, child := range node.Children {
		cp := NodeProgress(child)
		p.Complete += cp.Complete
		p.Total += cp.Total
	}
	return p
}

// TreeProgress returns progress for every non-leaf node in the tree,
// keyed by relative path.
func TreeProgress(tree *Tree) map[string]Progress {
	result := make(map[string]Progress)
	for path, node := range tree.All {
		if !node.IsLeaf {
			result[path] = NodeProgress(node)
		}
	}
	return result
}
