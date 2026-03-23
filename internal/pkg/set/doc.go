// Package set provides a generic unordered set backed by a map.
//
// Many places in wallfacer need to track unique items (workspace paths, task IDs,
// file names) and test membership efficiently. This package provides a simple
// [Set] type with Add, Remove, Has, and Items operations, avoiding repeated
// map[T]struct{} boilerplate throughout the codebase.
//
// # Connected packages
//
// No dependencies (not even stdlib). Consumed by [runner] (tracking board items,
// parsing ideation output, deduplicating workspace entries) and [workspace]
// (normalizing workspace groups). A simple utility with no ripple effects.
//
// # Usage
//
//	s := set.New("a", "b", "c")
//	s.Add("d")
//	if s.Has("a") { ... }
//	items := s.Items()
package set
