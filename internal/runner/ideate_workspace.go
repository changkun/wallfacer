package runner

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
	"changkun.de/x/wallfacer/prompts"
)

// ignoredChurnPrefixes lists path prefixes (relative to the workspace root) that
// are excluded from churn signal collection. These are vendor, generated, and
// artifact directories that produce noise without reflecting actionable source code.
var ignoredChurnPrefixes = []string{
	"ui/js/vendor/",
	"ui/js/generated/",
	"node_modules/",
	".git/",
	"vendor/",
	"dist/",
	"build/",
	".cache/",
}

// ignoredTodoPrefixes extends the common ignore list with paths that are
// additionally excluded from TODO signal collection. Prompt templates naturally
// contain TODO-like placeholder text that does not represent real work items.
// testdata/ directories contain fixture text that should not be treated as live TODOs.
var ignoredTodoPrefixes = []string{
	"ui/js/vendor/",
	"ui/js/generated/",
	"node_modules/",
	".git/",
	"vendor/",
	"dist/",
	"build/",
	".cache/",
	"prompts/",
	"testdata/",
}

// ignoredPathSuffixes lists file extension suffixes that indicate minified,
// generated, or lock-file assets. These are excluded regardless of directory.
// The ".lock" suffix covers Cargo.lock, yarn.lock, poetry.lock, Gemfile.lock,
// composer.lock, flake.lock, and any other "<name>.lock" dependency lock files.
// Files whose exact basename needs matching (e.g. go.sum, package-lock.json)
// are handled by ignoredChurnExactNames instead.
var ignoredPathSuffixes = []string{
	".min.js",
	".min.css",
	".pb.go",
	"_gen.go",
	"_generated.go",
	".lock",
}

// ignoredChurnExactNames lists exact file basenames excluded from both churn
// and TODO signal collection. Dependency lock files accumulate the highest
// commit counts in most repos (every dependency update touches them) yet carry
// zero signal about code quality or technical debt. Files whose names end in
// ".lock" are also caught by ignoredPathSuffixes; these entries provide
// explicit documentation and cover names that do not match the suffix rule
// (go.sum, package-lock.json, packages.lock.json, pnpm-lock.yaml).
var ignoredChurnExactNames = []string{
	"go.sum",
	"package-lock.json",
	"yarn.lock",
	"pnpm-lock.yaml",
	"Cargo.lock",
	"poetry.lock",
	"Gemfile.lock",
	"composer.lock",
	"flake.lock",
	"packages.lock.json",
}

// boostedPathPrefixes lists directory prefixes whose files are considered
// higher-value actionable source code. Files under these paths receive a 2×
// score multiplier so they rank ahead of vendor or low-signal paths.
var boostedPathPrefixes = []string{
	"internal/",
	"ui/partials/",
}

// boostedPathSuffixes lists file suffixes that receive a score boost. Test files
// adjacent to production code indicate both importance and improvement opportunity.
var boostedPathSuffixes = []string{
	"_test.go",
	".test.ts",
	".spec.ts",
	".test.js",
	".spec.js",
}

// IdeationIgnorePatterns is the canonical ordered list of path prefixes and
// exact basenames excluded from workspace signal collection. It is exposed via
// GET /api/config so callers can understand and reproduce the filtering logic
// without reading source code. Entries without a trailing "/" are exact
// basename matches (e.g. "go.sum"); entries with a trailing "/" are directory
// prefix matches; ".lock" is a filename suffix match.
var IdeationIgnorePatterns = func() []string {
	seen := make(map[string]bool)
	var result []string
	for _, p := range append(append([]string{}, ignoredChurnPrefixes...), "prompts/", "testdata/") {
		if !seen[p] {
			seen[p] = true
			result = append(result, p)
		}
	}
	for _, name := range ignoredChurnExactNames {
		if !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	if !seen[".lock"] {
		seen[".lock"] = true
		result = append(result, ".lock")
	}
	return result
}()

// isIgnoredChurnPath reports whether a workspace-relative file path should be
// excluded from churn signal collection.
func isIgnoredChurnPath(path string) bool {
	base := filepath.Base(path)
	for _, name := range ignoredChurnExactNames {
		if base == name {
			return true
		}
	}
	for _, sfx := range ignoredPathSuffixes {
		if strings.HasSuffix(path, sfx) {
			return true
		}
	}
	for _, pfx := range ignoredChurnPrefixes {
		if strings.HasPrefix(path, pfx) {
			return true
		}
	}
	return false
}

// isIgnoredTodoPath reports whether a workspace-relative file path should be
// excluded from TODO signal collection.
func isIgnoredTodoPath(path string) bool {
	base := filepath.Base(path)
	for _, name := range ignoredChurnExactNames {
		if base == name {
			return true
		}
	}
	for _, sfx := range ignoredPathSuffixes {
		if strings.HasSuffix(path, sfx) {
			return true
		}
	}
	for _, pfx := range ignoredTodoPrefixes {
		if strings.HasPrefix(path, pfx) {
			return true
		}
	}
	return false
}

// isBoostedPath reports whether a workspace-relative path should receive a score
// multiplier. Files in key source directories and adjacent test files are
// considered higher-value signals than equivalent-count vendor or generated paths.
func isBoostedPath(path string) bool {
	for _, pfx := range boostedPathPrefixes {
		if strings.HasPrefix(path, pfx) {
			return true
		}
	}
	// ui/js/ is boosted only outside the vendor/generated subdirectories.
	if strings.HasPrefix(path, "ui/js/") &&
		!strings.HasPrefix(path, "ui/js/vendor/") &&
		!strings.HasPrefix(path, "ui/js/generated/") {
		return true
	}
	for _, sfx := range boostedPathSuffixes {
		if strings.HasSuffix(path, sfx) {
			return true
		}
	}
	return false
}

// effectiveScore returns the weighted score for a signal entry. Boosted paths
// receive a 2× multiplier to rank them above vendor or artifact paths with
// equal raw counts.
func effectiveScore(score int, boosted bool) int {
	if boosted {
		return score * 2
	}
	return score
}

// workspaceBasename returns the last non-empty path component of a workspace
// directory path, handling trailing slashes correctly.
func workspaceBasename(ws string) string {
	ws = strings.TrimRight(ws, "/")
	return filepath.Base(ws)
}

// collectWorkspaceChurnSignals returns scored churn hotspots across all
// workspaces. Paths are filtered to exclude vendor/generated/artifact trees and
// minified assets. When multiple workspaces are active each path is namespaced
// with the workspace basename. Duplicate paths that appear in multiple workspaces
// are collapsed into a single scored entry. Results are capped at
// constants.MaxIdeationChurnSignals. The second return value is the total count of paths
// excluded by the ignore rules across all workspaces.
func (r *Runner) collectWorkspaceChurnSignals(ctx context.Context) ([]prompts.WorkspaceSignal, int) {
	workspaces := r.workspacesForRunner()
	multi := len(workspaces) > 1

	// Collapse across workspaces: same display path → sum scores.
	byDisplayPath := make(map[string]*prompts.WorkspaceSignal)
	totalFiltered := 0

	for _, workspace := range workspaces {
		raw, filtered := r.collectWorkspaceChurnSignalsForWorkspace(ctx, workspace, multi)
		totalFiltered += filtered
		for i := range raw {
			sig := &raw[i]
			if existing, ok := byDisplayPath[sig.DisplayPath]; ok {
				existing.Score += sig.Score
				// Re-derive reason from summed score.
				existing.Reason = fmt.Sprintf("%d commits", existing.Score)
			} else {
				cp := *sig
				byDisplayPath[sig.DisplayPath] = &cp
			}
		}
	}

	result := make([]prompts.WorkspaceSignal, 0, len(byDisplayPath))
	for _, sig := range byDisplayPath {
		result = append(result, *sig)
	}
	slices.SortFunc(result, func(a, b prompts.WorkspaceSignal) int {
		if c := cmp.Compare(effectiveScore(b.Score, b.Boosted), effectiveScore(a.Score, a.Boosted)); c != 0 {
			return c
		}
		return strings.Compare(a.DisplayPath, b.DisplayPath)
	})
	if len(result) > constants.MaxIdeationChurnSignals {
		result = result[:constants.MaxIdeationChurnSignals]
	}
	return result, totalFiltered
}

// collectWorkspaceChurnSignalsForWorkspace collects churn signals for a single
// workspace. It returns scored WorkspaceSignal entries (with DisplayPath already
// namespaced when multi is true) and the count of paths excluded by ignore rules.
//
// Fallback: when all paths in the workspace are filtered (e.g. a purely generated
// repo), the top 3 paths are included anyway so the caller always receives some
// signal rather than nothing. The fallback count is not added to filteredCount.
func (r *Runner) collectWorkspaceChurnSignalsForWorkspace(ctx context.Context, workspace string, multi bool) ([]prompts.WorkspaceSignal, int) {
	raw, err := r.runWorkspaceGitCommand(ctx, workspace, "log", "--name-only", "--pretty=format:",
		fmt.Sprintf("--since=%d.days.ago", constants.ChurnLookbackDays),
		"-n", strconv.Itoa(constants.MaxChurnCommits))
	if err != nil {
		return nil, 0
	}
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return nil, 0
	}

	counts := make(map[string]int)
	filteredCount := 0
	for _, line := range strings.Split(s, "\n") {
		file := strings.TrimSpace(line)
		if file == "" {
			continue
		}
		if isIgnoredChurnPath(file) {
			filteredCount++
			continue
		}
		counts[file]++
	}

	// Fallback: if every path was ignored, include the best paths regardless of
	// ignore rules so the caller always has some signal to work with.
	if len(counts) == 0 && filteredCount > 0 {
		all := make(map[string]int)
		for _, line := range strings.Split(s, "\n") {
			file := strings.TrimSpace(line)
			if file != "" {
				all[file]++
			}
		}
		type kv struct {
			path  string
			count int
		}
		var items []kv
		for p, c := range all {
			items = append(items, kv{p, c})
		}
		slices.SortFunc(items, func(a, b kv) int {
			if c := cmp.Compare(b.count, a.count); c != 0 {
				return c
			}
			return strings.Compare(a.path, b.path)
		})
		maxItems := 3
		if maxItems > len(items) {
			maxItems = len(items)
		}
		for i := 0; i < maxItems; i++ {
			counts[items[i].path] = items[i].count
		}
		// Reset filteredCount: we're surfacing fallback paths, not hiding them.
		filteredCount = 0
	}

	type ranked struct {
		path    string
		count   int
		boosted bool
	}
	var list []ranked
	for path, count := range counts {
		list = append(list, ranked{path, count, isBoostedPath(path)})
	}
	slices.SortFunc(list, func(a, b ranked) int {
		if c := cmp.Compare(effectiveScore(b.count, b.boosted), effectiveScore(a.count, a.boosted)); c != 0 {
			return c
		}
		return strings.Compare(a.path, b.path)
	})

	basename := workspaceBasename(workspace)
	out := make([]prompts.WorkspaceSignal, 0, len(list))
	for _, it := range list {
		displayPath := it.path
		if multi {
			displayPath = basename + "/" + it.path
		}
		out = append(out, prompts.WorkspaceSignal{
			DisplayPath: displayPath,
			Score:       it.count,
			Reason:      fmt.Sprintf("%d commits", it.count),
			Workspace:   basename,
			Boosted:     it.boosted,
		})
	}
	return out, filteredCount
}

// collectWorkspaceTodoSignals returns scored TODO/FIXME/XXX hotspots across all
// workspaces. Paths are filtered to exclude vendor/generated trees, minified assets,
// and prompt template files. Multi-workspace paths are namespaced, and duplicate
// paths across workspaces are collapsed into a single scored entry. Results are
// capped at constants.MaxIdeationTodoSignals. The second return value is the total excluded count.
func (r *Runner) collectWorkspaceTodoSignals(ctx context.Context) ([]prompts.WorkspaceSignal, int) {
	workspaces := r.workspacesForRunner()
	multi := len(workspaces) > 1

	byDisplayPath := make(map[string]*prompts.WorkspaceSignal)
	totalFiltered := 0

	for _, workspace := range workspaces {
		raw, filtered := r.collectWorkspaceTodoSignalsForWorkspace(ctx, workspace, multi)
		totalFiltered += filtered
		for i := range raw {
			sig := &raw[i]
			if existing, ok := byDisplayPath[sig.DisplayPath]; ok {
				existing.Score += sig.Score
				existing.Reason = fmt.Sprintf("%d markers", existing.Score)
			} else {
				cp := *sig
				byDisplayPath[sig.DisplayPath] = &cp
			}
		}
	}

	result := make([]prompts.WorkspaceSignal, 0, len(byDisplayPath))
	for _, sig := range byDisplayPath {
		result = append(result, *sig)
	}
	slices.SortFunc(result, func(a, b prompts.WorkspaceSignal) int {
		if c := cmp.Compare(effectiveScore(b.Score, b.Boosted), effectiveScore(a.Score, a.Boosted)); c != 0 {
			return c
		}
		return strings.Compare(a.DisplayPath, b.DisplayPath)
	})
	if len(result) > constants.MaxIdeationTodoSignals {
		result = result[:constants.MaxIdeationTodoSignals]
	}
	return result, totalFiltered
}

// collectWorkspaceTodoSignalsForWorkspace collects TODO/FIXME/XXX signals for a
// single workspace. Prompt template paths are always excluded even in fallback mode
// since they contain placeholder text rather than actionable work items.
func (r *Runner) collectWorkspaceTodoSignalsForWorkspace(ctx context.Context, workspace string, multi bool) ([]prompts.WorkspaceSignal, int) {
	raw, err := r.runWorkspaceGitCommand(ctx, workspace, "grep", "-n", "-E", "TODO|FIXME|XXX", "--", ".")
	if err != nil {
		return nil, 0
	}

	counts := make(map[string]int)
	filteredCount := 0
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		before, _, found := strings.Cut(trimmed, ":")
		if !found || before == "" {
			continue
		}
		if isIgnoredTodoPath(before) {
			filteredCount++
			continue
		}
		counts[before]++
	}

	// Fallback: if all paths were filtered, surface the best non-prompt paths.
	// Prompt template files (prompts/) are never included even as fallback because
	// they contain structural TODO-like text that does not represent real work.
	if len(counts) == 0 && filteredCount > 0 {
		all := make(map[string]int)
		for _, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			before, _, found := strings.Cut(trimmed, ":")
			if !found || before == "" {
				continue
			}
			// Always exclude prompt templates even in fallback.
			if strings.HasPrefix(before, "prompts/") {
				continue
			}
			all[before]++
		}
		type kv struct {
			path  string
			count int
		}
		var items []kv
		for p, c := range all {
			items = append(items, kv{p, c})
		}
		slices.SortFunc(items, func(a, b kv) int {
			if c := cmp.Compare(b.count, a.count); c != 0 {
				return c
			}
			return strings.Compare(a.path, b.path)
		})
		maxItems := 3
		if maxItems > len(items) {
			maxItems = len(items)
		}
		for i := 0; i < maxItems; i++ {
			counts[items[i].path] = items[i].count
		}
		filteredCount = 0
	}

	type ranked struct {
		path    string
		count   int
		boosted bool
	}
	var list []ranked
	for path, count := range counts {
		list = append(list, ranked{path, count, isBoostedPath(path)})
	}
	slices.SortFunc(list, func(a, b ranked) int {
		if c := cmp.Compare(effectiveScore(b.count, b.boosted), effectiveScore(a.count, a.boosted)); c != 0 {
			return c
		}
		return strings.Compare(a.path, b.path)
	})

	basename := workspaceBasename(workspace)
	out := make([]prompts.WorkspaceSignal, 0, len(list))
	for _, it := range list {
		displayPath := it.path
		if multi {
			displayPath = basename + "/" + it.path
		}
		out = append(out, prompts.WorkspaceSignal{
			DisplayPath: displayPath,
			Score:       it.count,
			Reason:      fmt.Sprintf("%d markers", it.count),
			Workspace:   basename,
			Boosted:     it.boosted,
		})
	}
	return out, filteredCount
}

func (r *Runner) runWorkspaceGitCommand(parentCtx context.Context, workspace string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parentCtx, constants.WorkspaceIdeationCommandTTL)
	defer cancel()
	return cmdexec.Git(workspace, args...).WithContext(ctx).OutputBytes()
}

func (r *Runner) workspacesForRunner() []string {
	var ws []string
	for _, raw := range r.workspaces {
		clean := strings.TrimSpace(raw)
		if clean == "" {
			continue
		}
		ws = append(ws, clean)
	}
	return ws
}
