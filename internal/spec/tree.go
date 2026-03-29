package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Node represents a spec document within the spec tree.
type Node struct {
	Spec     *Spec   // parsed spec document
	Parent   *Node   // nil for root-level specs
	Children []*Node // child specs from subdirectory
	IsLeaf   bool    // true if no children
	Depth    int     // 0 for root-level specs
}

// Tree holds the complete spec tree built from the filesystem.
type Tree struct {
	Roots []*Node            // top-level specs (depth 0)
	All   map[string]*Node   // all nodes indexed by relative path
	Errs  []error            // parse errors collected during tree building
}

// NodeAt looks up a node by its relative path.
func (t *Tree) NodeAt(path string) (*Node, bool) {
	n, ok := t.All[path]
	return n, ok
}

// Leaves returns all leaf nodes in the tree.
func (t *Tree) Leaves() []*Node {
	var leaves []*Node
	for _, n := range t.All {
		if n.IsLeaf {
			leaves = append(leaves, n)
		}
	}
	return leaves
}

// ByTrack returns root nodes belonging to the given track.
func (t *Tree) ByTrack(track Track) []*Node {
	var nodes []*Node
	for _, r := range t.Roots {
		if r.Spec != nil && r.Spec.Track == track {
			nodes = append(nodes, r)
		}
	}
	return nodes
}

// BuildTree walks the specs directory and assembles the spec tree.
// The specsDir should be the path to the top-level specs/ directory.
// Parse errors are collected in Tree.Errs rather than aborting the build.
func BuildTree(specsDir string) (*Tree, error) {
	tree := &Tree{
		All: make(map[string]*Node),
	}

	entries, err := os.ReadDir(specsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return tree, nil
		}
		return nil, fmt.Errorf("read specs dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		trackDir := filepath.Join(specsDir, entry.Name())
		roots, errs := scanDir(trackDir, specsDir, nil, 0)
		tree.Roots = append(tree.Roots, roots...)
		tree.Errs = append(tree.Errs, errs...)
		for _, r := range roots {
			indexNodes(tree.All, r)
		}
	}

	return tree, nil
}

// scanDir scans a directory for .md spec files, recursing into matching
// subdirectories for child specs. parent is nil for root-level specs.
func scanDir(dir, specsDir string, parent *Node, depth int) ([]*Node, []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, []error{fmt.Errorf("read dir %s: %w", dir, err)}
	}

	var nodes []*Node
	var errs []error

	// First pass: collect .md files.
	mdFiles := map[string]string{} // base name (without .md) -> full path
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".md")
		mdFiles[base] = filepath.Join(dir, e.Name())
	}

	// Second pass: build nodes from .md files and check for child directories.
	for base, mdPath := range mdFiles {
		relPath, _ := filepath.Rel(specsDir, mdPath)
		// Normalize to forward slashes for consistent keys.
		relPath = filepath.ToSlash(relPath)

		s, parseErr := ParseFile(mdPath)
		if parseErr != nil {
			errs = append(errs, fmt.Errorf("parse %s: %w", relPath, parseErr))
			continue
		}
		s.Path = relPath

		node := &Node{
			Spec:   s,
			Parent: parent,
			Depth:  depth,
			IsLeaf: true,
		}

		// Check for matching subdirectory.
		childDir := filepath.Join(dir, base)
		if info, statErr := os.Stat(childDir); statErr == nil && info.IsDir() {
			children, childErrs := scanDir(childDir, specsDir, node, depth+1)
			errs = append(errs, childErrs...)
			if len(children) > 0 {
				node.Children = children
				node.IsLeaf = false
			}
		}

		nodes = append(nodes, node)
	}

	// Third pass: find orphan directories (no matching .md file) and still
	// scan their children.
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, hasMD := mdFiles[e.Name()]; hasMD {
			continue
		}
		childDir := filepath.Join(dir, e.Name())
		children, childErrs := scanDir(childDir, specsDir, parent, depth)
		nodes = append(nodes, children...)
		errs = append(errs, childErrs...)
	}

	return nodes, errs
}

// indexNodes recursively adds a node and all its children to the index map.
func indexNodes(index map[string]*Node, node *Node) {
	if node.Spec != nil {
		index[node.Spec.Path] = node
	}
	for _, child := range node.Children {
		indexNodes(index, child)
	}
}
