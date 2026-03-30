// Package dag provides generic operations on directed acyclic graphs
// represented as adjacency lists.
package dag

import "slices"

// ReverseEdges inverts all edges in an adjacency list.
// For every edge a→b in adj, the result contains b→a.
func ReverseEdges[K comparable](adj map[K][]K) map[K][]K {
	rev := make(map[K][]K)
	for from, tos := range adj {
		for _, to := range tos {
			rev[to] = append(rev[to], from)
		}
		// Ensure every node appears as a key even if it has no reverse edges.
		if _, ok := rev[from]; !ok {
			rev[from] = nil
		}
	}
	return rev
}

// DetectCycles finds all cycles in a directed graph using DFS with coloring.
// Returns a slice of cycles, where each cycle is an ordered path that loops
// back to its start (e.g., [A, B, C, A]).
func DetectCycles[K comparable](adj map[K][]K) [][]K {
	type color int
	const (
		white color = iota
		gray
		black
	)

	colors := make(map[K]color)
	parent := make(map[K]K)
	var cycles [][]K

	var dfs func(node K)
	dfs = func(node K) {
		colors[node] = gray
		for _, next := range adj[node] {
			switch colors[next] {
			case white:
				parent[next] = node
				dfs(next)
			case gray:
				// Back edge — cycle found. Reconstruct path.
				cycle := []K{next, node}
				cur := node
				for cur != next {
					cur = parent[cur]
					cycle = append(cycle, cur)
				}
				slices.Reverse(cycle)
				cycles = append(cycles, cycle)
			}
		}
		colors[node] = black
	}

	for node := range adj {
		if colors[node] == white {
			dfs(node)
		}
	}
	return cycles
}

// Reachable returns the set of all nodes transitively reachable from start
// via BFS through adj. The start node itself is not included in the result.
func Reachable[K comparable](adj map[K][]K, start K) map[K]bool {
	visited := map[K]bool{start: true}
	queue := []K{start}
	result := make(map[K]bool)

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range adj[cur] {
			if visited[next] {
				continue
			}
			visited[next] = true
			result[next] = true
			queue = append(queue, next)
		}
	}
	return result
}
