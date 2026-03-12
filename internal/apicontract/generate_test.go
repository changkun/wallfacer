package apicontract

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// repoRoot returns the repository root directory by walking up from this
// source file. Tests in internal/apicontract are two levels below the root.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// thisFile = .../internal/apicontract/generate_test.go
	// Go up two directories to reach the repo root.
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}

// TestGeneratedRoutesJS_NotStale fails if ui/js/generated/routes.js does not
// match what GenerateRoutesJS() would produce from the current Routes slice.
// Run "make api-contract" to regenerate.
func TestGeneratedRoutesJS_NotStale(t *testing.T) {
	want := GenerateRoutesJS()

	path := filepath.Join(repoRoot(t), "ui", "js", "generated", "routes.js")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v\nRun 'make api-contract' to generate it.", path, err)
	}

	if string(got) != want {
		t.Errorf("ui/js/generated/routes.js is stale.\n"+
			"Run 'make api-contract' to regenerate from internal/apicontract/routes.go.\n"+
			"First differing byte found; want len=%d got len=%d", len(want), len(got))
	}
}

// TestGeneratedContractJSON_NotStale fails if docs/internals/api-contract.json
// does not match what GenerateContractJSON() would produce from the current Routes.
// Run "make api-contract" to regenerate.
func TestGeneratedContractJSON_NotStale(t *testing.T) {
	want, err := GenerateContractJSON()
	if err != nil {
		t.Fatalf("GenerateContractJSON: %v", err)
	}

	path := filepath.Join(repoRoot(t), "docs", "internals", "api-contract.json")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v\nRun 'make api-contract' to generate it.", path, err)
	}

	if string(got) != string(want) {
		t.Errorf("docs/internals/api-contract.json is stale.\n"+
			"Run 'make api-contract' to regenerate from internal/apicontract/routes.go.\n"+
			"want len=%d got len=%d", len(want), len(got))
	}
}

// TestGenerateRoutesJS_Deterministic verifies that calling GenerateRoutesJS
// twice yields the same output (no time stamps or non-deterministic content).
func TestGenerateRoutesJS_Deterministic(t *testing.T) {
	a := GenerateRoutesJS()
	b := GenerateRoutesJS()
	if a != b {
		t.Error("GenerateRoutesJS is not deterministic: two calls produced different output")
	}
}

// TestGeneratedTypesJS_NotStale fails if ui/js/generated/types.js does not
// match what GenerateJSTypes() would produce from the current jsTypeRegistry.
// Run "make api-contract" to regenerate.
func TestGeneratedTypesJS_NotStale(t *testing.T) {
	want := GenerateJSTypes()

	path := filepath.Join(repoRoot(t), "ui", "js", "generated", "types.js")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v\nRun 'make api-contract' to generate it.", path, err)
	}

	if string(got) != want {
		t.Errorf("ui/js/generated/types.js is stale.\n"+
			"Run 'make api-contract' to regenerate from internal/apicontract/generate.go.\n"+
			"First differing byte found; want len=%d got len=%d", len(want), len(got))
	}
}

// TestGenerateJSTypes_Deterministic verifies that calling GenerateJSTypes
// twice yields identical output (no timestamps or non-deterministic content).
func TestGenerateJSTypes_Deterministic(t *testing.T) {
	a := GenerateJSTypes()
	b := GenerateJSTypes()
	if a != b {
		t.Error("GenerateJSTypes is not deterministic: two calls produced different output")
	}
}

// TestRoutes_NoDuplicateNames verifies that every Route.Name is unique.
func TestRoutes_NoDuplicateNames(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range Routes {
		if seen[r.Name] {
			t.Errorf("duplicate Route.Name %q", r.Name)
		}
		seen[r.Name] = true
	}
}

// TestRoutes_NoEmptyFields verifies that required fields are non-empty.
func TestRoutes_NoEmptyFields(t *testing.T) {
	for _, r := range Routes {
		if r.Method == "" {
			t.Errorf("route %q has empty Method", r.Name)
		}
		if r.Pattern == "" {
			t.Errorf("route %q has empty Pattern", r.Name)
		}
		if r.Name == "" {
			t.Errorf("route with pattern %q has empty Name", r.Pattern)
		}
		if r.Description == "" {
			t.Errorf("route %q has empty Description", r.Name)
		}
		if len(r.Tags) == 0 {
			t.Errorf("route %q has no Tags", r.Name)
		}
	}
}

// TestJSMethodName_Derivation spot-checks the kebab/slash→camelCase derivation.
func TestJSMethodName_Derivation(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"rebase-on-main", "rebaseOnMain"},
		{"create-branch", "createBranch"},
		{"archive-done", "archiveDone"},
		{"generate-titles", "generateTitles"},
		{"generate-oversight", "generateOversight"},
		{"refine/logs", "refineLogs"},
		{"refine/apply", "refineApply"},
		{"refine/dismiss", "refineDismiss"},
		{"oversight/test", "oversightTest"},
		{"turn-usage", "turnUsage"},
		{"status", "status"},
		{"stream", "stream"},
		{"search", "search"},
	}
	for _, tc := range cases {
		got := kebabSlashToCamel(tc.input)
		if got != tc.want {
			t.Errorf("kebabSlashToCamel(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestBuildTaskPathExpr_Substitution verifies path variable substitution.
func TestBuildTaskPathExpr_Substitution(t *testing.T) {
	cases := []struct {
		pattern string
		want    string
	}{
		{
			"/api/tasks/{id}/diff",
			`"/api/tasks/" + id + "/diff"`,
		},
		{
			"/api/tasks/{id}",
			`"/api/tasks/" + id`,
		},
		{
			"/api/tasks/{id}/outputs/{filename}",
			`"/api/tasks/" + id + "/outputs/" + filename`,
		},
		{
			"/api/tasks/{id}/refine/logs",
			`"/api/tasks/" + id + "/refine/logs"`,
		},
	}
	for _, tc := range cases {
		got := buildTaskPathExpr(tc.pattern)
		if got != tc.want {
			t.Errorf("buildTaskPathExpr(%q)\n  got  %s\n  want %s", tc.pattern, got, tc.want)
		}
	}
}
