package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Severity classifies a validation result.
type Severity string

// Severity constants.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Result represents a single validation finding for a spec.
type Result struct {
	Path     string   // spec relative path
	Severity Severity // error or warning
	Rule     string   // rule identifier (e.g., "required-fields")
	Message  string   // human-readable description
}

// ValidateSpec runs all per-spec validation rules and returns all violations.
// The repoRoot is used to resolve depends_on and affects paths on disk.
// isLeaf indicates whether the spec has no children in the tree.
func ValidateSpec(s *Spec, repoRoot string, isLeaf bool) []Result {
	var results []Result

	results = append(results, checkRequiredFields(s)...)
	results = append(results, checkValidEnums(s)...)
	results = append(results, checkTrackMatchesPath(s)...)
	results = append(results, checkDateOrdering(s)...)
	results = append(results, checkNoSelfDependency(s)...)
	results = append(results, checkDispatchConsistency(s, isLeaf)...)
	results = append(results, checkDependsOnExist(s, repoRoot)...)
	results = append(results, checkAffectsExist(s, repoRoot)...)
	results = append(results, checkBodyNotEmpty(s)...)

	return results
}

func checkRequiredFields(s *Spec) []Result {
	var results []Result
	if s.Title == "" {
		results = append(results, Result{s.Path, SeverityError, "required-fields", "title is required"})
	}
	if s.Status == "" {
		results = append(results, Result{s.Path, SeverityError, "required-fields", "status is required"})
	}
	if s.Track == "" {
		results = append(results, Result{s.Path, SeverityError, "required-fields", "track is required"})
	}
	if s.Effort == "" {
		results = append(results, Result{s.Path, SeverityError, "required-fields", "effort is required"})
	}
	if s.Created.IsZero() {
		results = append(results, Result{s.Path, SeverityError, "required-fields", "created is required"})
	}
	if s.Updated.IsZero() {
		results = append(results, Result{s.Path, SeverityError, "required-fields", "updated is required"})
	}
	if s.Author == "" {
		results = append(results, Result{s.Path, SeverityError, "required-fields", "author is required"})
	}
	return results
}

func checkValidEnums(s *Spec) []Result {
	var results []Result
	if s.Status != "" && !slices.Contains(ValidStatuses(), s.Status) {
		results = append(results, Result{s.Path, SeverityError, "valid-status",
			fmt.Sprintf("invalid status %q", s.Status)})
	}
	if s.Track != "" && !slices.Contains(ValidTracks(), s.Track) {
		results = append(results, Result{s.Path, SeverityError, "valid-track",
			fmt.Sprintf("invalid track %q", s.Track)})
	}
	if s.Effort != "" && !slices.Contains(ValidEfforts(), s.Effort) {
		results = append(results, Result{s.Path, SeverityError, "valid-effort",
			fmt.Sprintf("invalid effort %q", s.Effort)})
	}
	return results
}

func checkTrackMatchesPath(s *Spec) []Result {
	if s.Track == "" || s.Path == "" {
		return nil
	}
	// Path should start with "<track>/" (relative to specs dir).
	parts := strings.SplitN(filepath.ToSlash(s.Path), "/", 2)
	if len(parts) < 2 {
		return nil
	}
	pathTrack := parts[0]
	if pathTrack != string(s.Track) {
		return []Result{{s.Path, SeverityError, "track-matches-path",
			fmt.Sprintf("track %q does not match path prefix %q", s.Track, pathTrack)}}
	}
	return nil
}

func checkDateOrdering(s *Spec) []Result {
	if s.Created.IsZero() || s.Updated.IsZero() {
		return nil
	}
	if s.Updated.Before(s.Created.Time) {
		return []Result{{s.Path, SeverityError, "date-ordering",
			"updated date is before created date"}}
	}
	return nil
}

func checkNoSelfDependency(s *Spec) []Result {
	for _, dep := range s.DependsOn {
		if dep == s.Path {
			return []Result{{s.Path, SeverityError, "no-self-dependency",
				"spec depends on itself"}}
		}
	}
	return nil
}

func checkDispatchConsistency(s *Spec, isLeaf bool) []Result {
	if !isLeaf && s.DispatchedTaskID != nil {
		return []Result{{s.Path, SeverityError, "dispatch-consistency",
			"non-leaf spec must not have dispatched_task_id"}}
	}
	return nil
}

func checkDependsOnExist(s *Spec, repoRoot string) []Result {
	if repoRoot == "" {
		return nil
	}
	var results []Result
	for _, dep := range s.DependsOn {
		full := filepath.Join(repoRoot, dep)
		if _, err := os.Stat(full); err != nil {
			results = append(results, Result{s.Path, SeverityError, "depends-on-exist",
				fmt.Sprintf("dependency %q does not exist", dep)})
		}
	}
	return results
}

func checkAffectsExist(s *Spec, repoRoot string) []Result {
	if repoRoot == "" {
		return nil
	}
	var results []Result
	for _, path := range s.Affects {
		full := filepath.Join(repoRoot, path)
		if _, err := os.Stat(full); err != nil {
			results = append(results, Result{s.Path, SeverityWarning, "affects-exist",
				fmt.Sprintf("affected path %q does not exist", path)})
		}
	}
	return results
}

// ValidateTree runs all per-spec and cross-spec validation rules on the tree.
func ValidateTree(tree *Tree, repoRoot string) []Result {
	var results []Result

	// Per-spec validation for each node.
	for _, node := range tree.All {
		if node.Spec != nil {
			results = append(results, ValidateSpec(node.Spec, repoRoot, node.IsLeaf)...)
		}
	}

	// Cross-spec rules.
	results = append(results, checkDAGAcyclic(tree)...)
	results = append(results, checkOrphanDirectories(tree, repoRoot)...)
	results = append(results, checkStatusConsistency(tree)...)
	results = append(results, checkStalePropagation(tree)...)
	results = append(results, checkTrackConsistency(tree)...)
	results = append(results, checkUniqueDispatches(tree)...)

	return results
}

// checkDAGAcyclic verifies the depends_on graph has no cycles using DFS coloring.
func checkDAGAcyclic(tree *Tree) []Result {
	type color int
	const (
		white color = iota // unvisited
		gray               // in-progress
		black              // done
	)

	colors := make(map[string]color)
	parent := make(map[string]string) // for cycle path reconstruction

	var results []Result

	var dfs func(path string)
	dfs = func(path string) {
		colors[path] = gray
		node, ok := tree.All[path]
		if !ok || node.Spec == nil {
			colors[path] = black
			return
		}
		for _, dep := range node.Spec.DependsOn {
			switch colors[dep] {
			case white:
				parent[dep] = path
				dfs(dep)
			case gray:
				// Back edge — cycle detected. Reconstruct path.
				cycle := []string{dep, path}
				cur := path
				for cur != dep {
					cur = parent[cur]
					cycle = append(cycle, cur)
				}
				// Reverse to get forward order.
				for i, j := 0, len(cycle)-1; i < j; i, j = i+1, j-1 {
					cycle[i], cycle[j] = cycle[j], cycle[i]
				}
				results = append(results, Result{
					Path:     path,
					Severity: SeverityError,
					Rule:     "dag-acyclic",
					Message:  fmt.Sprintf("dependency cycle: %s", strings.Join(cycle, " -> ")),
				})
			}
		}
		colors[path] = black
	}

	for path := range tree.All {
		if colors[path] == white {
			dfs(path)
		}
	}
	return results
}

// checkOrphanDirectories detects subdirectories without matching parent .md files.
// The tree builder already handles orphans by scanning their children, but this
// rule surfaces them as warnings for human attention.
func checkOrphanDirectories(tree *Tree, repoRoot string) []Result {
	if repoRoot == "" {
		return nil
	}

	var results []Result
	specsDir := filepath.Join(repoRoot, "specs")

	// Walk all nodes and check if their parent directory has a matching .md.
	for path := range tree.All {
		parts := strings.Split(filepath.ToSlash(path), "/")
		if len(parts) < 3 {
			// track/name.md — root level, no orphan possible.
			continue
		}
		// For track/parent/child.md, check that track/parent.md exists in tree.
		parentDir := strings.Join(parts[:len(parts)-1], "/")
		parentMD := parentDir + ".md"
		if _, ok := tree.All[parentMD]; !ok {
			// Check the filesystem too — maybe the .md failed to parse.
			fullParentMD := filepath.Join(specsDir, filepath.FromSlash(parentMD))
			if _, err := os.Stat(fullParentMD); err != nil {
				results = append(results, Result{
					Path:     path,
					Severity: SeverityWarning,
					Rule:     "no-orphan-directories",
					Message:  fmt.Sprintf("parent spec %q not found for subdirectory", parentMD),
				})
			}
		}
	}
	return results
}

// checkStatusConsistency warns when a complete non-leaf has incomplete leaves.
func checkStatusConsistency(tree *Tree) []Result {
	var results []Result
	for _, node := range tree.All {
		if node.IsLeaf || node.Spec == nil || node.Spec.Status != StatusComplete {
			continue
		}
		if hasIncompleteLeaf(node) {
			results = append(results, Result{
				Path:     node.Spec.Path,
				Severity: SeverityWarning,
				Rule:     "status-consistency",
				Message:  "complete non-leaf spec has incomplete leaves in subtree",
			})
		}
	}
	return results
}

func hasIncompleteLeaf(node *Node) bool {
	if node.IsLeaf {
		return node.Spec != nil && node.Spec.Status != StatusComplete
	}
	for _, child := range node.Children {
		if hasIncompleteLeaf(child) {
			return true
		}
	}
	return false
}

// checkStalePropagation warns when a stale spec has validated dependents.
func checkStalePropagation(tree *Tree) []Result {
	// Build reverse index: spec path -> list of dependents.
	reverse := make(map[string][]string)
	for path, node := range tree.All {
		if node.Spec == nil {
			continue
		}
		for _, dep := range node.Spec.DependsOn {
			reverse[dep] = append(reverse[dep], path)
		}
	}

	var results []Result
	for path, node := range tree.All {
		if node.Spec == nil || node.Spec.Status != StatusStale {
			continue
		}
		for _, dependent := range reverse[path] {
			depNode, ok := tree.All[dependent]
			if !ok || depNode.Spec == nil {
				continue
			}
			if depNode.Spec.Status == StatusValidated {
				results = append(results, Result{
					Path:     dependent,
					Severity: SeverityWarning,
					Rule:     "stale-propagation",
					Message:  fmt.Sprintf("depends on stale spec %q — review needed", path),
				})
			}
		}
	}
	return results
}

// checkTrackConsistency is handled by per-spec checkTrackMatchesPath — no
// additional tree-level logic needed beyond what per-spec validation covers.
func checkTrackConsistency(_ *Tree) []Result {
	return nil
}

// checkUniqueDispatches ensures no two specs share the same dispatched_task_id.
func checkUniqueDispatches(tree *Tree) []Result {
	seen := make(map[string]string) // dispatch ID -> first spec path
	var results []Result
	for path, node := range tree.All {
		if node.Spec == nil || node.Spec.DispatchedTaskID == nil {
			continue
		}
		id := *node.Spec.DispatchedTaskID
		if first, ok := seen[id]; ok {
			results = append(results, Result{
				Path:     path,
				Severity: SeverityError,
				Rule:     "unique-dispatches",
				Message:  fmt.Sprintf("dispatched_task_id %q already used by %q", id, first),
			})
		} else {
			seen[id] = path
		}
	}
	return results
}

func checkBodyNotEmpty(s *Spec) []Result {
	if s.Status == StatusVague || s.Status == "" {
		return nil
	}
	if strings.TrimSpace(s.Body) == "" {
		return []Result{{s.Path, SeverityWarning, "body-not-empty",
			"spec beyond vague status should have content"}}
	}
	return nil
}
