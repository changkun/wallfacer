package spec

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	gentree "latere.ai/x/wallfacer/internal/pkg/tree"
)

// Node is a spec document within the spec tree.
type Node = gentree.Node[string, *Spec]

// Tree holds the complete spec tree built from the filesystem.
type Tree struct {
	*gentree.Tree[string, *Spec]
	Errs []error // parse errors collected during tree building
}

// ByTrack returns root nodes belonging to the given track.
func (t *Tree) ByTrack(track string) []*Node {
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
//
// Specs are freeform: a spec may live as a loose .md file directly under
// specs/ (no track) or inside a (possibly nested) track folder. Both appear
// in the tree; the only top-level file excluded is README.md (the index).
func BuildTree(specsDir string) (*Tree, error) {
	tree := &Tree{Tree: gentree.New[string, *Spec]()}

	if _, err := os.ReadDir(specsDir); err != nil {
		if os.IsNotExist(err) {
			return tree, nil
		}
		return nil, fmt.Errorf("read specs dir: %w", err)
	}

	// Scan specs/ as a single directory: scanDir handles loose .md files,
	// matching child folders, and orphan track folders uniformly. The
	// .archive/ subtree is skipped here and scanned separately so its specs
	// surface at their logical (un-archived) paths.
	tree.Errs = append(tree.Errs, scanDir(tree, specsDir, specsDir, nil)...)

	// Second pass: relocated archived specs under specs/.archive/. They are
	// keyed by their logical path (the .archive/ segment stripped) so every
	// reference and the UI keep working; their status is forced archived.
	archiveDir := filepath.Join(specsDir, archiveSegment)
	if info, err := os.Stat(archiveDir); err == nil && info.IsDir() {
		tree.Errs = append(tree.Errs, scanArchiveDir(tree, archiveDir, specsDir)...)
	}

	return tree, nil
}

// scanArchiveDir walks specs/.archive/ and adds each archived spec to the tree
// at its logical path (physical .archive/ location stripped), forcing archived
// status and recording the physical path. Specs are added parent-before-child
// (sorted by logical path) so an archived child attaches to its parent —
// which may itself be live (already in the tree) or archived.
func scanArchiveDir(tree *Tree, archiveDir, specsDir string) []error {
	var errs []error
	var mdPaths []string
	walkErr := filepath.WalkDir(archiveDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") || d.Name() == "README.md" {
			return nil
		}
		mdPaths = append(mdPaths, p)
		return nil
	})
	if walkErr != nil {
		errs = append(errs, fmt.Errorf("scan archive dir %s: %w", archiveDir, walkErr))
	}

	type archEntry struct{ mdPath, physRel, logical string }
	entries := make([]archEntry, 0, len(mdPaths))
	for _, mdPath := range mdPaths {
		rel, _ := filepath.Rel(specsDir, mdPath)
		physRel := "specs/" + filepath.ToSlash(rel)
		entries = append(entries, archEntry{mdPath, physRel, LogicalPath(physRel)})
	}
	slices.SortFunc(entries, func(a, b archEntry) int { return strings.Compare(a.logical, b.logical) })

	for _, e := range entries {
		if _, exists := tree.All[e.logical]; exists {
			errs = append(errs, fmt.Errorf("archived spec %s collides with a live spec at %s", e.physRel, e.logical))
			continue
		}
		s, parseErr := ParseFile(e.mdPath)
		if parseErr != nil {
			if errors.Is(parseErr, ErrMissingFrontmatter) {
				s = docNode(e.mdPath)
			} else {
				errs = append(errs, fmt.Errorf("parse %s: %w", e.physRel, parseErr))
				continue
			}
		}
		s.Path = e.logical
		s.PhysicalPath = e.physRel
		s.Track = trackFromPath(e.logical)
		if !s.Doc {
			s.Status = StatusArchived // physical location is authoritative
		}

		if parentKey := archiveLogicalParent(e.logical); parentKey != "" {
			if _, ok := tree.All[parentKey]; ok {
				tree.Add(e.logical, s, &parentKey)
				continue
			}
		}
		tree.Add(e.logical, s, nil)
	}
	return errs
}

// archiveLogicalParent returns the logical parent key for a logical spec path,
// or "" when the spec sits at the track root. specs/a/b/c.md -> specs/a/b.md.
func archiveLogicalParent(logical string) string {
	dir := path.Dir(logical)
	if dir == "specs" || dir == "." || dir == "/" {
		return ""
	}
	return dir + ".md"
}

// docNode synthesizes a render-only node for a frontmatter-less markdown file.
// It has no status and no lifecycle; the title is the file's first `# H1`,
// falling back to a title-cased filename. Slices are non-nil to match the
// shape ParseBytes guarantees for real specs.
func docNode(mdPath string) *Spec {
	title, _ := readFirstH1(mdPath, TitleFromFilename(mdPath))
	return &Spec{
		Title:     title,
		Doc:       true,
		DependsOn: []string{},
		Affects:   []string{},
	}
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
		if e.Name() == "README.md" {
			continue // index/readme, not a spec
		}
		base := strings.TrimSuffix(e.Name(), ".md")
		mdFiles[base] = filepath.Join(dir, e.Name())
	}

	// Sort bases for deterministic tree order across calls.
	bases := make([]string, 0, len(mdFiles))
	for base := range mdFiles {
		bases = append(bases, base)
	}
	slices.Sort(bases)

	// Second pass: build nodes from .md files and check for child directories.
	for _, base := range bases {
		mdPath := mdFiles[base]
		relPath, _ := filepath.Rel(specsDir, mdPath)
		relPath = "specs/" + filepath.ToSlash(relPath)

		s, parseErr := ParseFile(mdPath)
		if parseErr != nil {
			// A frontmatter-less file is not an error: surface it as a
			// render-only doc node instead of dropping it silently. Genuine
			// parse failures (malformed YAML) still go to tree.Errs.
			if errors.Is(parseErr, ErrMissingFrontmatter) {
				s = docNode(mdPath)
			} else {
				errs = append(errs, fmt.Errorf("parse %s: %w", relPath, parseErr))
				continue
			}
		}
		s.Path = relPath
		s.Track = trackFromPath(relPath)

		tree.Add(relPath, s, parentKey)

		// Check for matching subdirectory.
		childDir := filepath.Join(dir, base)
		if info, statErr := os.Stat(childDir); statErr == nil && info.IsDir() {
			childErrs := scanDir(tree, childDir, specsDir, &relPath)
			errs = append(errs, childErrs...)

			// Reorder children based on the order they appear as
			// markdown links in the parent spec's body.
			reorderChildren(tree, relPath, s.Body)
		}
	}

	// Third pass: find orphan directories (no matching .md file) and still
	// scan their children.
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if e.Name() == archiveSegment {
			continue // relocated archived specs are scanned separately
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

// mdLinkRe matches markdown links with .md targets: [text](path.md)
var mdLinkRe = regexp.MustCompile(`\[[^\]]*\]\(([^)]+\.md)\)`)

// reorderChildren reorders a parent node's children to match the order
// child specs appear as markdown links in the parent's body. Children
// not referenced in the body keep their existing (alphabetical) order
// and are appended after the referenced ones.
func reorderChildren(tree *Tree, parentKey, body string) {
	parent, ok := tree.NodeAt(parentKey)
	if !ok || len(parent.Children) <= 1 {
		return
	}

	matches := mdLinkRe.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return
	}

	// Resolve link targets relative to the parent spec's directory
	// to obtain tree keys.
	parentDir := filepath.Dir(parentKey)
	childByKey := make(map[string]*Node, len(parent.Children))
	for _, child := range parent.Children {
		childByKey[child.Key] = child
	}

	seen := make(map[string]bool)
	ordered := make([]*Node, 0, len(parent.Children))
	for _, m := range matches {
		resolved := filepath.ToSlash(filepath.Join(parentDir, m[1]))
		if child, ok := childByKey[resolved]; ok && !seen[resolved] {
			ordered = append(ordered, child)
			seen[resolved] = true
		}
	}

	// Append unreferenced children in their original order.
	for _, child := range parent.Children {
		if !seen[child.Key] {
			ordered = append(ordered, child)
		}
	}

	parent.Children = ordered
}
