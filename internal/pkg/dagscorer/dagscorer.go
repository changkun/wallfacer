// Package dagscorer computes the longest downstream chain length (critical
// path score) for nodes in a directed acyclic graph.
package dagscorer

// Score returns the length of the longest downstream dependency chain from
// start. The children function returns the direct dependents of a node.
// An unknown node (one not returned by any children call) scores 0.
// Cycles are detected and broken (cycle nodes count as 1).
func Score[Node comparable](start Node, children func(Node) []Node) int {
	return score(start, children, make(map[Node]int), make(map[Node]bool))
}

func score[Node comparable](id Node, children func(Node) []Node, memo map[Node]int, visiting map[Node]bool) int {
	if v, ok := memo[id]; ok {
		return v
	}
	if visiting[id] {
		return 1 // cycle guard
	}
	visiting[id] = true
	defer func() { visiting[id] = false }()

	best := 0
	for _, child := range children(id) {
		if s := score(child, children, memo, visiting); s > best {
			best = s
		}
	}
	result := 1 + best
	memo[id] = result
	return result
}
