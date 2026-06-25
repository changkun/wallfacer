package spec

import (
	"errors"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"latere.ai/x/wallfacer/internal/pkg/dag"
)

// Stale propagation fans out staleness to specs impacted by an event (a chat
// edit or a task completion) through two complementary channels:
//
//   - Channel 1, depends_on reverse: a spec that explicitly depends on the
//     event source is a direct (single-hop) dependent. Transitive dependents
//     are picked up by the next event in the chain, not cascaded here.
//   - Channel 2, affects overlap: a spec that touches the same files as the
//     event is implicitly coupled even when no depends_on edge was declared.
//
// Results from both channels are unioned, the source is excluded, and archived
// specs never participate (Adjacency prunes them; the affects index skips
// them). FanOutStale then writes status: stale for every legal transition.

// normalizeAffect collapses trailing slashes and OS path separators so that
// "internal/sandbox/" and "internal/sandbox" map to the same key.
func normalizeAffect(e string) string {
	return strings.TrimRight(filepath.ToSlash(e), "/")
}

// affectContains reports whether dir contains path along the path tree, where
// equality counts as containment. Siblings never contain each other.
func affectContains(dir, path string) bool {
	return dir == path || strings.HasPrefix(path, dir+"/")
}

// affectsOverlap reports whether two affects entries overlap: one contains the
// other. The relation is symmetric.
func affectsOverlap(a, b string) bool {
	return affectContains(a, b) || affectContains(b, a)
}

// affectsToSpecs is the reverse affects index: normalized affects entry → the
// set of spec paths that declare it. Archived and doc nodes are excluded, so
// every spec in the index is a live participant.
func affectsToSpecs(tree *Tree) map[string][]string {
	idx := make(map[string][]string)
	for path, node := range tree.All {
		if node.Value == nil || node.Value.Doc {
			continue
		}
		if node.Value.Status == StatusArchived {
			continue
		}
		seen := make(map[string]bool, len(node.Value.Affects))
		for _, e := range node.Value.Affects {
			key := normalizeAffect(e)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			idx[key] = append(idx[key], path)
		}
	}
	return idx
}

// DependsOnImpact returns the direct (single-hop) dependents of source via the
// depends_on graph. Archived specs are pruned by Adjacency. The source itself
// is never included.
func DependsOnImpact(tree *Tree, source string) []string {
	reverse := dag.ReverseEdges(Adjacency(tree))
	var out []string
	for _, dep := range reverse[source] {
		if dep != source {
			out = append(out, dep)
		}
	}
	return out
}

// AffectsImpactFromDiff returns specs whose affects entries contain any of the
// changed files. Used on task done, where the real task diff is available, so
// matching is precise: an entry impacts the spec if it contains a changed file
// (a declared directory or the exact file). The source is excluded.
func AffectsImpactFromDiff(tree *Tree, changedFiles []string, source string) []string {
	idx := affectsToSpecs(tree)
	files := make([]string, 0, len(changedFiles))
	for _, f := range changedFiles {
		if n := normalizeAffect(f); n != "" {
			files = append(files, n)
		}
	}

	impacted := make(map[string]bool)
	for entry, specs := range idx {
		for _, f := range files {
			if affectContains(entry, f) {
				for _, p := range specs {
					if p != source {
						impacted[p] = true
					}
				}
				break
			}
		}
	}
	return setToSorted(impacted)
}

// AffectsImpactFromSpec returns specs whose affects entries overlap the
// source's declared affects. Used on chat edits, where the commit touches
// specs/ only and no code diff exists, so matching is symmetric containment
// against the source's declared affects. The source is excluded.
func AffectsImpactFromSpec(tree *Tree, source string) []string {
	node, ok := tree.All[source]
	if !ok || node.Value == nil {
		return nil
	}
	srcAffects := make([]string, 0, len(node.Value.Affects))
	for _, e := range node.Value.Affects {
		if n := normalizeAffect(e); n != "" {
			srcAffects = append(srcAffects, n)
		}
	}

	idx := affectsToSpecs(tree)
	impacted := make(map[string]bool)
	for entry, specs := range idx {
		for _, sa := range srcAffects {
			if affectsOverlap(sa, entry) {
				for _, p := range specs {
					if p != source {
						impacted[p] = true
					}
				}
				break
			}
		}
	}
	return setToSorted(impacted)
}

// FanOutStale transitions each impacted spec to stale. resolve maps a spec's
// tree path to its filesystem path; a spec that resolves to "" is skipped.
// Archived specs and illegal transitions (including same-to-same on an
// already-stale spec) are skipped. Status-only writes do not touch code, so
// they trigger no further affects fan-out. Best-effort: a write failure is
// collected and reported but does not stop the remaining writes. Returns the
// paths actually transitioned, sorted.
func FanOutStale(tree *Tree, impacted []string, resolve func(string) string, now time.Time) ([]string, error) {
	sorted := slices.Clone(impacted)
	sort.Strings(sorted)

	var applied []string
	var errs []error
	for _, path := range sorted {
		node, ok := tree.All[path]
		if !ok || node.Value == nil {
			continue
		}
		if node.Value.Status == StatusArchived {
			continue
		}
		if StatusMachine.Validate(node.Value.Status, StatusStale) != nil {
			continue
		}
		abs := resolve(path)
		if abs == "" {
			continue
		}
		if err := UpdateFrontmatter(abs, map[string]any{
			"status":  string(StatusStale),
			"updated": now,
		}); err != nil {
			errs = append(errs, err)
			continue
		}
		applied = append(applied, path)
	}
	return applied, errors.Join(errs...)
}

func setToSorted(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
