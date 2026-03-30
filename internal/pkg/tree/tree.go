// Package tree provides a generic rooted tree with parent-child
// relationships and a key-based index.
package tree

import "iter"

// Node represents a value within a tree.
type Node[K comparable, V any] struct {
	Key      K             // unique identifier for this node
	Value    V             // payload
	Parent   *Node[K, V]   // nil for root nodes
	Children []*Node[K, V] // child nodes
	IsLeaf   bool          // true if no children
	Depth    int           // 0 for root nodes
}

// Tree holds a set of root nodes and an index of all nodes by key.
type Tree[K comparable, V any] struct {
	Roots []*Node[K, V]     // top-level nodes (depth 0)
	All   map[K]*Node[K, V] // all nodes indexed by key
}

// New creates an empty tree.
func New[K comparable, V any]() *Tree[K, V] {
	return &Tree[K, V]{All: make(map[K]*Node[K, V])}
}

// NodeAt looks up a node by key.
func (t *Tree[K, V]) NodeAt(key K) (*Node[K, V], bool) {
	n, ok := t.All[key]
	return n, ok
}

// Leaves returns an iterator over all leaf nodes in the tree.
func (t *Tree[K, V]) Leaves() iter.Seq[*Node[K, V]] {
	return func(yield func(*Node[K, V]) bool) {
		for _, n := range t.All {
			if n.IsLeaf {
				if !yield(n) {
					return
				}
			}
		}
	}
}

// Walk returns an iterator over every node in depth-first pre-order.
func (t *Tree[K, V]) Walk() iter.Seq[*Node[K, V]] {
	return func(yield func(*Node[K, V]) bool) {
		for _, root := range t.Roots {
			if !walkNode(root, yield) {
				return
			}
		}
	}
}

func walkNode[K comparable, V any](n *Node[K, V], yield func(*Node[K, V]) bool) bool {
	if !yield(n) {
		return false
	}
	for _, child := range n.Children {
		if !walkNode(child, yield) {
			return false
		}
	}
	return true
}

// Add inserts a node into the tree with the given key, value, and optional
// parent key. If parentKey is provided and found, the node is added as a
// child; otherwise it becomes a root. Returns the created node.
func (t *Tree[K, V]) Add(key K, value V, parentKey *K) *Node[K, V] {
	node := &Node[K, V]{
		Key:    key,
		Value:  value,
		IsLeaf: true,
	}

	if parentKey != nil {
		if parent, ok := t.All[*parentKey]; ok {
			node.Parent = parent
			node.Depth = parent.Depth + 1
			parent.Children = append(parent.Children, node)
			parent.IsLeaf = false
		} else {
			t.Roots = append(t.Roots, node)
		}
	} else {
		t.Roots = append(t.Roots, node)
	}

	t.All[key] = node
	return node
}
