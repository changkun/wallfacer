package cli

import (
	"os"
	"path/filepath"
	"testing"

	"changkun.de/x/wallfacer/internal/spec"
)

// writeSpecFile stages a spec file with the given frontmatter + body on
// disk, creating parent directories as needed.
func writeSpecFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// validSpec returns a minimally-valid spec body. Per-spec validation will
// pass (modulo affects-paths-exist warnings when the referenced directory
// does not exist on disk — use affects: [] to avoid those).
func validSpec(title string) string {
	return "---\n" +
		"title: " + title + "\n" +
		"status: drafted\n" +
		"depends_on: []\n" +
		"affects: []\n" +
		"effort: small\n" +
		"created: 2026-04-12\n" +
		"updated: 2026-04-12\n" +
		"author: test\n" +
		"dispatched_task_id: null\n" +
		"---\n\n" +
		"# " + title + "\n"
}

func TestPathFilter_AbsoluteAndRelative(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "specs", "local", "foo.md")
	// The filter keys are canonicalised paths relative to specsDir's parent.
	filter := pathFilter([]string{abs, "specs/local/bar.md"}, filepath.Join(dir, "specs"))
	if filter == nil {
		t.Fatal("expected non-nil filter for non-empty inputs")
	}
	if !filter["specs/local/foo.md"] {
		t.Errorf("filter missing specs/local/foo.md, got keys: %v", keys(filter))
	}
	if !filter["specs/local/bar.md"] {
		t.Errorf("filter missing specs/local/bar.md, got keys: %v", keys(filter))
	}
}

func TestPathFilter_Empty(t *testing.T) {
	if got := pathFilter(nil, "specs"); got != nil {
		t.Errorf("empty input should yield nil, got %v", got)
	}
}

func TestFilterResults_MatchesPath(t *testing.T) {
	results := []spec.Result{
		{Path: "specs/a.md", Severity: spec.SeverityError, Rule: "r1"},
		{Path: "specs/b.md", Severity: spec.SeverityWarning, Rule: "r2"},
		{Path: "specs/a.md", Severity: spec.SeverityError, Rule: "r3"},
	}
	got := filterResults(results, map[string]bool{"specs/a.md": true})
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	for _, r := range got {
		if r.Path != "specs/a.md" {
			t.Errorf("unexpected path %q in filtered results", r.Path)
		}
	}
}

func TestFilterSeverity_ErrorsOnly(t *testing.T) {
	results := []spec.Result{
		{Severity: spec.SeverityError, Rule: "r1"},
		{Severity: spec.SeverityWarning, Rule: "r2"},
		{Severity: spec.SeverityError, Rule: "r3"},
	}
	got := filterSeverity(results, spec.SeverityError)
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	for _, r := range got {
		if r.Severity != spec.SeverityError {
			t.Errorf("warning leaked into errors-only filter: %+v", r)
		}
	}
}

func TestCountSeverities(t *testing.T) {
	results := []spec.Result{
		{Severity: spec.SeverityError},
		{Severity: spec.SeverityError},
		{Severity: spec.SeverityWarning},
	}
	e, w := countSeverities(results)
	if e != 2 || w != 1 {
		t.Errorf("got (errors=%d, warnings=%d), want (2, 1)", e, w)
	}
}

func TestSpecValidate_CleanTreeBuildsAndValidates(t *testing.T) {
	dir := t.TempDir()
	specsDir := filepath.Join(dir, "specs")
	// BuildTree requires specs to live under a track directory
	// (specs/<track>/<name>.md), not directly under specs/.
	writeSpecFile(t, filepath.Join(specsDir, "local", "one.md"), validSpec("One"))
	writeSpecFile(t, filepath.Join(specsDir, "local", "two.md"), validSpec("Two"))

	tree, err := spec.BuildTree(specsDir)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}
	if len(tree.All) != 2 {
		t.Errorf("tree has %d specs, want 2", len(tree.All))
	}
	results := spec.ValidateTree(tree, dir)
	errs, warns := countSeverities(results)
	if errs != 0 {
		t.Errorf("expected no errors on clean tree, got %d: %+v", errs, results)
	}
	// Warnings may still show up from unrelated per-spec rules (e.g.
	// body-not-empty on minimal specs); they shouldn't fail the run.
	_ = warns
}

func TestSpecValidate_DetectsInvalidStatus(t *testing.T) {
	dir := t.TempDir()
	specsDir := filepath.Join(dir, "specs")
	// Inject an invalid status enum; BuildTree parses it, ValidateTree flags it.
	bad := "---\n" +
		"title: Bad\n" +
		"status: frobnicated\n" +
		"depends_on: []\n" +
		"affects: []\n" +
		"effort: small\n" +
		"created: 2026-04-12\n" +
		"updated: 2026-04-12\n" +
		"author: test\n" +
		"dispatched_task_id: null\n" +
		"---\n\n# Bad\n"
	writeSpecFile(t, filepath.Join(specsDir, "local", "bad.md"), bad)

	tree, err := spec.BuildTree(specsDir)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}
	results := spec.ValidateTree(tree, dir)
	var hit bool
	for _, r := range results {
		if r.Rule == "valid-status" && r.Severity == spec.SeverityError {
			hit = true
		}
	}
	if !hit {
		t.Errorf("expected a valid-enums error, got %+v", results)
	}
}

// keys returns the sorted keys of a string→bool map for error-message
// readability when a filter-membership check fails.
func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
