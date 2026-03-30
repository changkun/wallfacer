package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gentree "changkun.de/x/wallfacer/internal/pkg/tree"
)

// Node is a spec document within the spec tree.
type Node = gentree.Node[string, *Spec]

// Tree holds the complete spec tree built from the filesystem.
type Tree struct {
	*gentree.Tree[string, *Spec]
	Errs []error // parse errors collected during tree building
}

// ByTrack returns root nodes belonging to the given track.
func (t *Tree) ByTrack(track Track) []*Node {
	var nodes []*Node
	for _, r := range t.Roots {
		if r.Value != nil && r.Value.Track == track {
			nodes = append(nodes, r)
		}
	}
	return nodes
}

// BuildTree walks the specs directory and assembles the spec tree.
// The specsDir should be the path to the top-level specs/ directory.
// Parse errors are collected in Tree.Errs rather than aborting the build.
func BuildTree(specsDir string) (*Tree, error) {
	tree := &Tree{Tree: gentree.New[string, *Spec]()}

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
		errs := scanDir(tree, trackDir, specsDir, nil)
		tree.Errs = append(tree.Errs, errs...)
	}

	return tree, nil
}

// scanDir scans a directory for .md spec files, recursing into matching
// subdirectories for child specs.
func scanDir(tree *Tree, dir, specsDir string, parentKey *string) []error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []error{fmt.Errorf("read dir %s: %w", dir, err)}
	}

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
		relPath = filepath.ToSlash(relPath)

		s, parseErr := ParseFile(mdPath)
		if parseErr != nil {
			errs = append(errs, fmt.Errorf("parse %s: %w", relPath, parseErr))
			continue
		}
		s.Path = relPath

		tree.Add(relPath, s, parentKey)

		// Check for matching subdirectory.
		childDir := filepath.Join(dir, base)
		if info, statErr := os.Stat(childDir); statErr == nil && info.IsDir() {
			childErrs := scanDir(tree, childDir, specsDir, &relPath)
			errs = append(errs, childErrs...)
		}
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
		childErrs := scanDir(tree, childDir, specsDir, parentKey)
		errs = append(errs, childErrs...)
	}

	return errs
}
