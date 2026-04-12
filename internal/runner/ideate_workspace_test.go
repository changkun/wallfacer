package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/constants"
)

// commitFileInRepo creates a file at relPath inside repoDir and commits it.
// It creates any necessary parent directories.
func commitFileInRepo(t *testing.T, repoDir, relPath, content, message string) {
	t.Helper()
	fullPath := filepath.Join(repoDir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatalf("commitFileInRepo: mkdir %s: %v", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatalf("commitFileInRepo: write %s: %v", fullPath, err)
	}
	gitRun(t, repoDir, "add", relPath)
	gitRun(t, repoDir, "commit", "-m", message)
}

// ---------------------------------------------------------------------------
// isIgnoredChurnPath / isIgnoredTodoPath unit tests
// ---------------------------------------------------------------------------

func TestIsIgnoredChurnPath(t *testing.T) {
	cases := []struct {
		path    string
		ignored bool
	}{
		// Ignored vendor/generated trees
		{"ui/js/vendor/sortable.min.js", true},
		{"ui/js/generated/bundle.js", true},
		{"ui/js/build/lib/clipboard.js", true},
		{"node_modules/lodash/index.js", true},
		{".git/config", true},
		{"vendor/github.com/foo/bar/bar.go", true},
		{"dist/main.js", true},
		{"build/output.js", true},
		// Ignored minified suffixes
		{"static/app.min.js", true},
		{"static/styles.min.css", true},
		{"proto/foo.pb.go", true},
		{"gen/code_generated.go", true},
		// Lock files — exact basename matches
		{"go.sum", true},
		{"package-lock.json", true},
		{"pnpm-lock.yaml", true},
		{"packages.lock.json", true},
		// Lock files in subdirectories
		{"subdir/go.sum", true},
		{"subdir/package-lock.json", true},
		// Lock files matched by .lock suffix
		{"yarn.lock", true},
		{"Cargo.lock", true},
		{"poetry.lock", true},
		{"Gemfile.lock", true},
		{"composer.lock", true},
		{"flake.lock", true},
		{"some/custom.lock", true},
		// Non-ignored source paths
		{"internal/runner/ideate.go", false},
		{"ui/js/board.js", false}, // ui/js/ but not vendor/generated
		{"ui/partials/card.html", false},
		{"main.go", false},
		{"server.go", false},
		{"internal/handler/config.go", false},
	}
	for _, tc := range cases {
		got := isIgnoredChurnPath(tc.path)
		if got != tc.ignored {
			t.Errorf("isIgnoredChurnPath(%q) = %v; want %v", tc.path, got, tc.ignored)
		}
	}
}

func TestIsIgnoredTodoPath(t *testing.T) {
	cases := []struct {
		path    string
		ignored bool
	}{
		// Ignored vendor/generated trees (same as churn)
		{"ui/js/vendor/sortable.min.js", true},
		{"ui/js/build/lib/clipboard.js", true},
		{"node_modules/foo/index.js", true},
		{"vendor/dep/file.go", true},
		// Additionally excluded for TODO
		{"prompts/ideation.tmpl", true},
		{"prompts/commit.tmpl", true},
		{"testdata/fixture.txt", true},
		// Lock files — exact basename matches
		{"go.sum", true},
		{"package-lock.json", true},
		{"subdir/go.sum", true},
		// Lock files matched by .lock suffix
		{"yarn.lock", true},
		{"Cargo.lock", true},
		{"some/custom.lock", true},
		// Non-ignored source paths
		{"internal/runner/ideate.go", false},
		{"ui/js/board.js", false},
		{"main.go", false},
	}
	for _, tc := range cases {
		got := isIgnoredTodoPath(tc.path)
		if got != tc.ignored {
			t.Errorf("isIgnoredTodoPath(%q) = %v; want %v", tc.path, got, tc.ignored)
		}
	}
}

func TestIsBoostedPath(t *testing.T) {
	cases := []struct {
		path    string
		boosted bool
	}{
		{"internal/runner/ideate.go", true},
		{"internal/handler/config.go", true},
		{"ui/partials/card.html", true},
		{"ui/js/board.js", true},              // ui/js/ non-vendor
		{"ui/js/ideation.js", true},           // ui/js/ non-vendor
		{"internal/runner/foo_test.go", true}, // _test.go suffix
		{"handler_test.go", true},
		// Not boosted
		{"ui/js/vendor/sortable.js", false},     // under vendor
		{"ui/js/generated/bundle.js", false},    // under generated
		{"ui/js/build/lib/clipboard.js", false}, // under build
		{"main.go", false},
		{"server.go", false},
		{"README.md", false},
	}
	for _, tc := range cases {
		got := isBoostedPath(tc.path)
		if got != tc.boosted {
			t.Errorf("isBoostedPath(%q) = %v; want %v", tc.path, got, tc.boosted)
		}
	}
}

// ---------------------------------------------------------------------------
// collectWorkspaceChurnSignals — integration tests using real git repos
// ---------------------------------------------------------------------------

// TestChurnSignalsIgnoreVendorAndMinifiedPaths verifies that vendor, generated,
// and minified files are excluded from churn signals even when they have high
// commit counts. Commit counts for filtered paths should be reflected in filteredCount.
func TestChurnSignalsIgnoreVendorAndMinifiedPaths(t *testing.T) {
	repo := setupTestRepo(t)

	// Source file that should appear.
	commitFileInRepo(t, repo, "internal/runner/foo.go", "package runner\n", "add source file")

	// Vendor and minified files committed multiple times — should be filtered out.
	for i := 0; i < 5; i++ {
		commitFileInRepo(t, repo, "ui/js/vendor/sortable.min.js",
			fmt.Sprintf("// commit %d\n", i), fmt.Sprintf("update vendor %d", i))
		commitFileInRepo(t, repo, "node_modules/pkg/index.js",
			fmt.Sprintf("// commit %d\n", i), fmt.Sprintf("update node_modules %d", i))
	}

	_, r := setupTestRunner(t, []string{repo})
	signals, filtered := r.collectWorkspaceChurnSignals(context.Background())

	// No vendor/minified paths should appear.
	for _, sig := range signals {
		if strings.Contains(sig.DisplayPath, "vendor") {
			t.Errorf("vendor path %q must not appear in churn signals", sig.DisplayPath)
		}
		if strings.Contains(sig.DisplayPath, "node_modules") {
			t.Errorf("node_modules path %q must not appear in churn signals", sig.DisplayPath)
		}
		if strings.HasSuffix(sig.DisplayPath, ".min.js") {
			t.Errorf("minified path %q must not appear in churn signals", sig.DisplayPath)
		}
	}

	// filteredCount should be positive (many ignored commits).
	if filtered == 0 {
		t.Error("expected filteredCount > 0 for vendor/minified commits; got 0")
	}

	// Source file must appear.
	var found bool
	for _, sig := range signals {
		if sig.DisplayPath == "internal/runner/foo.go" {
			found = true
			break
		}
	}
	if !found {
		var paths []string
		for _, s := range signals {
			paths = append(paths, s.DisplayPath)
		}
		t.Errorf("expected internal/runner/foo.go in churn signals; got: %v", paths)
	}
}

// TestChurnSignalsIgnoreLockFiles verifies that dependency lock files are excluded
// from churn signals even when they have high commit counts, while real source
// files continue to appear.
func TestChurnSignalsIgnoreLockFiles(t *testing.T) {
	repo := setupTestRepo(t)

	// Real source file that must appear in results.
	commitFileInRepo(t, repo, "internal/main.go", "package main\n", "add source file")

	// Lock files committed multiple times — should all be filtered out.
	for i := 0; i < 5; i++ {
		commitFileInRepo(t, repo, "go.sum",
			fmt.Sprintf("h1:hash%d\n", i), fmt.Sprintf("update go.sum %d", i))
		commitFileInRepo(t, repo, "package-lock.json",
			fmt.Sprintf(`{"lockfileVersion":%d}`, i), fmt.Sprintf("update package-lock.json %d", i))
	}

	_, r := setupTestRunner(t, []string{repo})
	signals, filtered := r.collectWorkspaceChurnSignals(context.Background())

	// No lock files should appear.
	for _, sig := range signals {
		if sig.DisplayPath == "go.sum" || strings.HasSuffix(sig.DisplayPath, "/go.sum") {
			t.Errorf("go.sum must not appear in churn signals; got %q", sig.DisplayPath)
		}
		if sig.DisplayPath == "package-lock.json" || strings.HasSuffix(sig.DisplayPath, "/package-lock.json") {
			t.Errorf("package-lock.json must not appear in churn signals; got %q", sig.DisplayPath)
		}
	}

	// filteredCount should be positive.
	if filtered == 0 {
		t.Error("expected filteredCount > 0 for lock file commits; got 0")
	}

	// Source file must appear.
	var found bool
	for _, sig := range signals {
		if sig.DisplayPath == "internal/main.go" {
			found = true
			break
		}
	}
	if !found {
		var paths []string
		for _, s := range signals {
			paths = append(paths, s.DisplayPath)
		}
		t.Errorf("expected internal/main.go in churn signals; got: %v", paths)
	}
}

// TestChurnSignalsBoostedPathsRankHigher verifies that files in internal/ receive
// a score multiplier and rank above non-boosted files with equal commit counts.
func TestChurnSignalsBoostedPathsRankHigher(t *testing.T) {
	repo := setupTestRepo(t)

	// Both files get 2 commits. internal/ should rank first due to boost.
	for i := 0; i < 2; i++ {
		commitFileInRepo(t, repo, "internal/runner/foo.go",
			fmt.Sprintf("// v%d\n", i), fmt.Sprintf("internal commit %d", i))
		commitFileInRepo(t, repo, "main.go",
			fmt.Sprintf("// main v%d\n", i), fmt.Sprintf("main commit %d", i))
	}

	_, r := setupTestRunner(t, []string{repo})
	signals, _ := r.collectWorkspaceChurnSignals(context.Background())

	if len(signals) < 2 {
		t.Fatalf("expected at least 2 churn signals, got %d", len(signals))
	}
	if signals[0].DisplayPath != "internal/runner/foo.go" {
		t.Errorf("expected boosted internal/runner/foo.go to rank first; got %q", signals[0].DisplayPath)
	}
	if !signals[0].Boosted {
		t.Errorf("expected Boosted=true for internal/runner/foo.go")
	}
}

// TestChurnSignalsSingleWorkspaceRelativePaths verifies that in single-workspace
// mode the DisplayPath contains no workspace basename prefix.
func TestChurnSignalsSingleWorkspaceRelativePaths(t *testing.T) {
	repo := setupTestRepo(t)
	commitFileInRepo(t, repo, "internal/foo.go", "package foo\n", "add internal file")

	_, r := setupTestRunner(t, []string{repo})
	signals, _ := r.collectWorkspaceChurnSignals(context.Background())

	basename := filepath.Base(repo)
	for _, sig := range signals {
		if strings.HasPrefix(sig.DisplayPath, basename+"/") {
			t.Errorf("single-workspace DisplayPath %q must not have basename prefix", sig.DisplayPath)
		}
	}
}

// TestChurnSignalsMultiWorkspaceNamespacing verifies that when multiple workspaces
// are active each signal's DisplayPath is prefixed with the workspace basename
// and the Workspace field is populated.
func TestChurnSignalsMultiWorkspaceNamespacing(t *testing.T) {
	repo1 := setupTestRepo(t)
	repo2 := setupTestRepo(t)

	commitFileInRepo(t, repo1, "internal/foo.go", "package foo\n", "add foo in repo1")
	commitFileInRepo(t, repo2, "internal/bar.go", "package bar\n", "add bar in repo2")

	_, r := setupTestRunner(t, []string{repo1, repo2})
	signals, _ := r.collectWorkspaceChurnSignals(context.Background())

	base1 := filepath.Base(repo1)
	base2 := filepath.Base(repo2)

	if len(signals) == 0 {
		t.Fatal("expected churn signals from multi-workspace repos; got none")
	}
	for _, sig := range signals {
		if !strings.HasPrefix(sig.DisplayPath, base1+"/") && !strings.HasPrefix(sig.DisplayPath, base2+"/") {
			t.Errorf("multi-workspace DisplayPath %q must start with one of: %q, %q",
				sig.DisplayPath, base1+"/", base2+"/")
		}
		if sig.Workspace == "" {
			t.Errorf("signal %q must have non-empty Workspace in multi-workspace mode", sig.DisplayPath)
		}
	}
}

// TestChurnSignalsDuplicatePathsCollapsed verifies that the same display path
// only appears once in the final results (no duplicate entries per workspace).
func TestChurnSignalsDuplicatePathsCollapsed(t *testing.T) {
	repo1 := setupTestRepo(t)
	repo2 := setupTestRepo(t)

	// Same relative path in both repos.
	commitFileInRepo(t, repo1, "README.md", "# Repo1\n", "update README")
	commitFileInRepo(t, repo2, "README.md", "# Repo2\n", "update README")

	_, r := setupTestRunner(t, []string{repo1, repo2})
	signals, _ := r.collectWorkspaceChurnSignals(context.Background())

	// Each display path must appear at most once in the result.
	seen := make(map[string]int)
	for _, sig := range signals {
		seen[sig.DisplayPath]++
	}
	for path, count := range seen {
		if count > 1 {
			t.Errorf("DisplayPath %q appears %d times; expected exactly once", path, count)
		}
	}
}

// TestChurnSignalsFallbackWhenAllIgnored verifies that when every committed file
// path matches an ignore rule, the collector returns non-empty signals rather
// than an empty slice, so the advisor always receives some context.
func TestChurnSignalsFallbackWhenAllIgnored(t *testing.T) {
	repo := setupTestRepo(t)

	// Commit only files in ignored directories.
	commitFileInRepo(t, repo, "vendor/dep/file.go", "package dep\n", "add vendor")
	commitFileInRepo(t, repo, "ui/js/vendor/lib.js", "var x = 1;\n", "add js vendor")
	commitFileInRepo(t, repo, "node_modules/pkg/index.js", "module.exports = {};\n", "add node_modules")

	_, r := setupTestRunner(t, []string{repo})
	signals, _ := r.collectWorkspaceChurnSignals(context.Background())

	if len(signals) == 0 {
		t.Error("expected fallback signals when all committed paths are normally ignored; got none")
	}
}

// TestChurnSignalsCap verifies that the collector returns at most
// constants.MaxIdeationChurnSignals entries even when the repo has more hot files.
func TestChurnSignalsCap(t *testing.T) {
	repo := setupTestRepo(t)

	// Create more files than the cap.
	for i := 0; i < constants.MaxIdeationChurnSignals+3; i++ {
		commitFileInRepo(t, repo, fmt.Sprintf("internal/file%d.go", i),
			"package foo\n", fmt.Sprintf("add file %d", i))
	}

	_, r := setupTestRunner(t, []string{repo})
	signals, _ := r.collectWorkspaceChurnSignals(context.Background())

	if len(signals) > constants.MaxIdeationChurnSignals {
		t.Errorf("expected at most %d churn signals, got %d", constants.MaxIdeationChurnSignals, len(signals))
	}
}

// ---------------------------------------------------------------------------
// collectWorkspaceTodoSignals — integration tests using real git repos
// ---------------------------------------------------------------------------

// TestTodoSignalsPromptFilesExcluded verifies that prompts/*.tmpl files are not
// included in TODO signals even when they have more TODO markers than source files.
func TestTodoSignalsPromptFilesExcluded(t *testing.T) {
	repo := setupTestRepo(t)

	// Prompt template with many TODO markers.
	var promptContent strings.Builder
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&promptContent, "// TODO: placeholder text %d\n", i)
	}
	commitFileInRepo(t, repo, "prompts/ideation.tmpl", promptContent.String(), "add prompt with TODOs")

	// Real source file with fewer TODOs.
	commitFileInRepo(t, repo, "internal/runner/handler.go",
		"package runner\n// TODO: implement this\n// TODO: add tests\n",
		"add source with TODOs")

	_, r := setupTestRunner(t, []string{repo})
	signals, filtered := r.collectWorkspaceTodoSignals(context.Background())

	// Prompt file must not appear.
	for _, sig := range signals {
		if strings.HasPrefix(sig.DisplayPath, "prompts/") {
			t.Errorf("prompt file %q must not appear in TODO signals", sig.DisplayPath)
		}
	}

	// filteredCount must be positive (prompt file was excluded).
	if filtered == 0 {
		t.Error("expected filteredCount > 0; prompt file TODOs should be counted as filtered")
	}

	// Source file must appear.
	var found bool
	for _, sig := range signals {
		if sig.DisplayPath == "internal/runner/handler.go" {
			found = true
			break
		}
	}
	if !found {
		var paths []string
		for _, s := range signals {
			paths = append(paths, s.DisplayPath)
		}
		t.Errorf("expected internal/runner/handler.go in TODO signals; got: %v", paths)
	}
}

// TestTodoSignalsVendorFilesExcluded verifies that vendor/node_modules paths
// are filtered from TODO signals and reflected in filteredCount.
func TestTodoSignalsVendorFilesExcluded(t *testing.T) {
	repo := setupTestRepo(t)

	commitFileInRepo(t, repo, "vendor/dep/file.go",
		"package dep\n// TODO: vendor todo\n// FIXME: also here\n", "add vendor with TODOs")
	commitFileInRepo(t, repo, "internal/runner/foo.go",
		"package runner\n// TODO: real work item\n", "add source with TODO")

	_, r := setupTestRunner(t, []string{repo})
	signals, filtered := r.collectWorkspaceTodoSignals(context.Background())

	for _, sig := range signals {
		if strings.HasPrefix(sig.DisplayPath, "vendor/") {
			t.Errorf("vendor path %q must not appear in TODO signals", sig.DisplayPath)
		}
	}
	if filtered == 0 {
		t.Error("expected filteredCount > 0 for vendor file TODOs")
	}
}

// TestTodoSignalsIgnoreLockFiles verifies that dependency lock files are excluded
// from TODO signals even if they contain TODO-like text, while real source files
// with TODO markers continue to appear.
func TestTodoSignalsIgnoreLockFiles(t *testing.T) {
	repo := setupTestRepo(t)

	// Lock files with TODO-like content — should be filtered out.
	commitFileInRepo(t, repo, "go.sum",
		"# TODO: this is not a real work item\nh1:abc123\n", "add go.sum with TODO-like text")
	commitFileInRepo(t, repo, "package-lock.json",
		`{"name":"app","lockfileVersion":3,"requires":true}`, "add package-lock.json")

	// Real source file with actual TODO markers — must appear.
	commitFileInRepo(t, repo, "internal/main.go",
		"package main\n// TODO: implement feature\n// FIXME: fix this\n", "add source with TODOs")

	_, r := setupTestRunner(t, []string{repo})
	signals, filtered := r.collectWorkspaceTodoSignals(context.Background())

	// No lock files should appear.
	for _, sig := range signals {
		if sig.DisplayPath == "go.sum" || strings.HasSuffix(sig.DisplayPath, "/go.sum") {
			t.Errorf("go.sum must not appear in TODO signals; got %q", sig.DisplayPath)
		}
		if sig.DisplayPath == "package-lock.json" || strings.HasSuffix(sig.DisplayPath, "/package-lock.json") {
			t.Errorf("package-lock.json must not appear in TODO signals; got %q", sig.DisplayPath)
		}
	}

	// filteredCount should be positive (go.sum was filtered).
	if filtered == 0 {
		t.Error("expected filteredCount > 0 for lock file TODOs; got 0")
	}

	// Source file must appear.
	var found bool
	for _, sig := range signals {
		if sig.DisplayPath == "internal/main.go" {
			found = true
			break
		}
	}
	if !found {
		var paths []string
		for _, s := range signals {
			paths = append(paths, s.DisplayPath)
		}
		t.Errorf("expected internal/main.go in TODO signals; got: %v", paths)
	}
}

// TestTodoSignalsFallbackWhenAllIgnored verifies that when every file with a TODO
// marker is in an ignored directory (except prompts/), the collector falls back
// to returning those files rather than emitting nothing.
func TestTodoSignalsFallbackWhenAllIgnored(t *testing.T) {
	repo := setupTestRepo(t)

	// Commit a file in vendor/ (ignored) with a TODO — not a prompt file.
	commitFileInRepo(t, repo, "vendor/dep/important.go",
		"package dep\n// TODO: this is a real todo in vendor\n", "add vendor TODO")

	_, r := setupTestRunner(t, []string{repo})
	signals, _ := r.collectWorkspaceTodoSignals(context.Background())

	// Fallback should surface something rather than returning empty.
	if len(signals) == 0 {
		t.Error("expected fallback signals when all TODO paths are normally ignored; got none")
	}
}

// TestTodoSignalsPromptOnlyRepoGracefulEmpty verifies that when a repo has TODO
// markers only in prompt template files (permanently excluded even in fallback),
// the collector returns an empty slice without crashing.
func TestTodoSignalsPromptOnlyRepoGracefulEmpty(t *testing.T) {
	repo := setupTestRepo(t)

	commitFileInRepo(t, repo, "prompts/custom.tmpl",
		"// TODO: placeholder\n// FIXME: another placeholder\n", "add prompt TODO")

	_, r := setupTestRunner(t, []string{repo})
	// Must not panic. Returning empty slice is acceptable.
	signals, _ := r.collectWorkspaceTodoSignals(context.Background())
	_ = signals // nil or empty is fine
}

// TestTodoSignalsMultiWorkspaceNamespacing verifies that in multi-workspace mode
// TODO signal paths are prefixed with the workspace basename.
func TestTodoSignalsMultiWorkspaceNamespacing(t *testing.T) {
	repo1 := setupTestRepo(t)
	repo2 := setupTestRepo(t)

	commitFileInRepo(t, repo1, "internal/foo.go",
		"// TODO: from repo1\n", "add TODO in repo1")
	commitFileInRepo(t, repo2, "internal/bar.go",
		"// TODO: from repo2\n", "add TODO in repo2")

	_, r := setupTestRunner(t, []string{repo1, repo2})
	signals, _ := r.collectWorkspaceTodoSignals(context.Background())

	base1 := filepath.Base(repo1)
	base2 := filepath.Base(repo2)

	if len(signals) == 0 {
		t.Fatal("expected TODO signals from multi-workspace repos; got none")
	}
	for _, sig := range signals {
		if !strings.HasPrefix(sig.DisplayPath, base1+"/") && !strings.HasPrefix(sig.DisplayPath, base2+"/") {
			t.Errorf("multi-workspace TODO path %q must start with one of: %q, %q",
				sig.DisplayPath, base1+"/", base2+"/")
		}
	}
}

// TestTodoSignalsCap verifies that the collector returns at most
// constants.MaxIdeationTodoSignals entries even when the repo has many files with TODOs.
func TestTodoSignalsCap(t *testing.T) {
	repo := setupTestRepo(t)

	for i := 0; i < constants.MaxIdeationTodoSignals+3; i++ {
		commitFileInRepo(t, repo, fmt.Sprintf("internal/file%d.go", i),
			fmt.Sprintf("package foo\n// TODO: item %d\n", i),
			fmt.Sprintf("add file %d", i))
	}

	_, r := setupTestRunner(t, []string{repo})
	signals, _ := r.collectWorkspaceTodoSignals(context.Background())

	if len(signals) > constants.MaxIdeationTodoSignals {
		t.Errorf("expected at most %d TODO signals, got %d", constants.MaxIdeationTodoSignals, len(signals))
	}
}

// ---------------------------------------------------------------------------
// WorkspaceSignal field correctness
// ---------------------------------------------------------------------------

// TestSignalReasonFormat verifies that churn signal Reason strings use the
// "N commits" format and TODO signal Reason strings use "N markers" format.
func TestSignalReasonFormat(t *testing.T) {
	repo := setupTestRepo(t)

	// Churn: commit the same file twice.
	commitFileInRepo(t, repo, "internal/foo.go", "package foo\n// v1\n", "commit 1")
	commitFileInRepo(t, repo, "internal/foo.go", "package foo\n// v2\n", "commit 2")
	// TODO: two markers in the same file.
	commitFileInRepo(t, repo, "internal/bar.go", "package bar\n// TODO: one\n// TODO: two\n", "add TODOs")

	_, r := setupTestRunner(t, []string{repo})

	churnSigs, _ := r.collectWorkspaceChurnSignals(context.Background())
	for _, sig := range churnSigs {
		if !strings.HasSuffix(sig.Reason, "commits") {
			t.Errorf("churn signal Reason %q should end with 'commits'", sig.Reason)
		}
	}

	todoSigs, _ := r.collectWorkspaceTodoSignals(context.Background())
	for _, sig := range todoSigs {
		if !strings.HasSuffix(sig.Reason, "markers") {
			t.Errorf("TODO signal Reason %q should end with 'markers'", sig.Reason)
		}
	}
}

// ---------------------------------------------------------------------------
// IdeationIgnorePatterns
// ---------------------------------------------------------------------------

// TestIdeationIgnorePatterns verifies that the exported pattern list includes
// the canonical vendor/generated/prompt paths, lock file exact names, the .lock
// suffix, and contains no duplicates.
func TestIdeationIgnorePatterns(t *testing.T) {
	must := []string{
		// Directory prefixes
		"ui/js/vendor/",
		"ui/js/generated/",
		"node_modules/",
		".git/",
		"vendor/",
		"prompts/",
		// Lock file exact basenames
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
		// Lock file suffix
		".lock",
	}
	for _, want := range must {
		var found bool
		for _, p := range IdeationIgnorePatterns {
			if p == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("IdeationIgnorePatterns is missing expected pattern %q", want)
		}
	}
	// Verify no duplicates.
	seen := make(map[string]bool)
	for _, p := range IdeationIgnorePatterns {
		if seen[p] {
			t.Errorf("IdeationIgnorePatterns contains duplicate entry %q", p)
		}
		seen[p] = true
	}
}

// ---------------------------------------------------------------------------
// Churn lookback window
// ---------------------------------------------------------------------------

// TestChurnSignalsExcludeOldCommits verifies that commits older than
// constants.ChurnLookbackDays are excluded from churn signals, while recent commits
// are included.
func TestChurnSignalsExcludeOldCommits(t *testing.T) {
	repo := setupTestRepo(t)

	// Commit a file with a backdated timestamp (91 days ago, outside the window).
	oldDate := time.Now().AddDate(0, 0, -91).Format(time.RFC3339)
	backdatedEnv := append(os.Environ(),
		"GIT_COMMITTER_DATE="+oldDate,
		"GIT_AUTHOR_DATE="+oldDate,
	)

	oldFile := "internal/old_file.go"
	fullOldPath := filepath.Join(repo, filepath.FromSlash(oldFile))
	if err := os.MkdirAll(filepath.Dir(fullOldPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullOldPath, []byte("package foo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	addCmd := exec.Command("git", "-C", repo, "add", oldFile)
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v\n%s", err, out)
	}
	commitCmd := exec.Command("git", "-C", repo, "commit", "-m", "old commit")
	commitCmd.Env = backdatedEnv
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit (backdated) failed: %v\n%s", err, out)
	}

	// Commit a recent file with the current date.
	commitFileInRepo(t, repo, "internal/recent_file.go", "package foo\n", "recent commit")

	_, r := setupTestRunner(t, []string{repo})
	signals, _ := r.collectWorkspaceChurnSignals(context.Background())

	var paths []string
	for _, sig := range signals {
		paths = append(paths, sig.DisplayPath)
	}

	// The old file must not appear — it falls outside the lookback window.
	for _, p := range paths {
		if strings.Contains(p, "old_file.go") {
			t.Errorf("churn signals contain old_file.go from 91 days ago; expected it to be excluded by %d-day window",
				constants.ChurnLookbackDays)
		}
	}

	// The recent file must appear.
	var found bool
	for _, p := range paths {
		if strings.Contains(p, "recent_file.go") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("churn signals do not contain recent_file.go; got paths: %v", paths)
	}
}
