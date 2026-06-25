// Package registry holds generic helpers for merging a built-in catalog with
// user-authored items keyed by a unique slug. It is shared by the agents and
// flow stores, which otherwise duplicated the same merge and lookup logic.
package registry

import "fmt"

// MergeUnique returns builtins followed by user items, keyed by slug. Built-ins
// win: a user item whose slug collides with a built-in yields an error rather
// than silently overriding it. mark, if non-nil, is applied to each built-in
// before it is appended (e.g. to stamp a Builtin flag). kind names the item
// type for the collision error ("agent", "flow").
func MergeUnique[T any](kind string, builtins, user []T, slugOf func(T) string, mark func(*T)) ([]T, error) {
	seen := make(map[string]bool, len(builtins))
	all := make([]T, 0, len(builtins)+len(user))
	for _, b := range builtins {
		if mark != nil {
			mark(&b)
		}
		seen[slugOf(b)] = true
		all = append(all, b)
	}
	for _, u := range user {
		if seen[slugOf(u)] {
			return nil, fmt.Errorf("user %s %q shadows a built-in slug; rename the file", kind, slugOf(u))
		}
		seen[slugOf(u)] = true
		all = append(all, u)
	}
	return all, nil
}

// ContainsSlug reports whether any item in items has the given slug.
func ContainsSlug[T any](items []T, slug string, slugOf func(T) string) bool {
	for i := range items {
		if slugOf(items[i]) == slug {
			return true
		}
	}
	return false
}
