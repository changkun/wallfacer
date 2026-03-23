// Package dagscorer computes the longest downstream dependency chain
// (critical path score) in a directed acyclic graph.
//
// Task dependencies in wallfacer form a DAG. When deciding which backlog task
// to promote next, the system uses the critical path score to prioritize tasks
// that unblock the longest chain of dependents. The scorer uses DFS with
// memoization and includes cycle detection to handle malformed dependency graphs
// gracefully.
//
// # Connected packages
//
// No dependencies (not even stdlib). Consumed by [store] for computing dependency
// graph priority scores during task promotion ordering. Changes to the scoring
// algorithm affect auto-promoter task selection.
//
// # Usage
//
//	score := dagscorer.Score(startNode, func(n Node) []Node {
//	    return n.Children()
//	})
package dagscorer
