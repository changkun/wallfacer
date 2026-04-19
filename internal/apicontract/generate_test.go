package apicontract

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	}
	for _, tc := range cases {
		got := buildTaskPathExpr(tc.pattern)
		if got != tc.want {
			t.Errorf("buildTaskPathExpr(%q)\n  got  %s\n  want %s", tc.pattern, got, tc.want)
		}
	}
}

// TestRoute_FullPattern verifies that FullPattern concatenates Method and Pattern
// with a space separator, matching the Go 1.22+ ServeMux pattern syntax.
func TestRoute_FullPattern(t *testing.T) {
	r := Route{Method: "GET", Pattern: "/api/tasks"}
	got := r.FullPattern()
	want := "GET /api/tasks"
	if got != want {
		t.Errorf("FullPattern() = %q, want %q", got, want)
	}
}

// TestRoute_FullPattern_AllRoutes ensures every route in the canonical Routes
// slice produces a non-empty FullPattern that starts with its HTTP method.
func TestRoute_FullPattern_AllRoutes(t *testing.T) {
	for _, r := range Routes {
		fp := r.FullPattern()
		if fp == "" {
			t.Errorf("Route %q FullPattern() = empty string", r.Name)
		}
		if fp[0:len(r.Method)] != r.Method {
			t.Errorf("FullPattern() %q does not start with method %q", fp, r.Method)
		}
	}
}

// TestKebabSlashToCamel_Empty verifies that an empty string returns empty.
func TestKebabSlashToCamel_Empty(t *testing.T) {
	got := kebabSlashToCamel("")
	if got != "" {
		t.Errorf("kebabSlashToCamel(%q) = %q, want %q", "", got, "")
	}
}

// TestJsMethodName_ExplicitJSName verifies that Route.JSName takes precedence.
func TestJsMethodName_ExplicitJSName(t *testing.T) {
	r := Route{Method: "GET", Pattern: "/api/env", JSName: "get"}
	got := jsMethodName(r, "env")
	if got != "get" {
		t.Errorf("jsMethodName() = %q, want %q", got, "get")
	}
}

// TestJsMethodName_NamespaceRoot returns empty when no suffix and no JSName.
func TestJsMethodName_NamespaceRoot(t *testing.T) {
	r := Route{Method: "GET", Pattern: "/api/env"}
	got := jsMethodName(r, "env")
	if got != "" {
		t.Errorf("jsMethodName() = %q, want %q", got, "")
	}
}

// TestJsTaskMethodName_ExplicitJSName verifies JSName takes precedence for task routes.
func TestJsTaskMethodName_ExplicitJSName(t *testing.T) {
	r := Route{Method: "POST", Pattern: "/api/tasks/{id}/sync", JSName: "syncMethod"}
	got := jsTaskMethodName(r)
	if got != "syncMethod" {
		t.Errorf("jsTaskMethodName() = %q, want %q", got, "syncMethod")
	}
}

// TestJsTaskMethodName_RootTaskRoute returns empty for /api/tasks/{id} without JSName.
func TestJsTaskMethodName_RootTaskRoute(t *testing.T) {
	r := Route{Method: "PATCH", Pattern: "/api/tasks/{id}"}
	got := jsTaskMethodName(r)
	if got != "" {
		t.Errorf("jsTaskMethodName() = %q, want %q", got, "")
	}
}

// TestNeedsQuoting_EdgeCases tests the quoting helper for identifiers.
func TestNeedsQuoting_EdgeCases(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"", true},
		{"validName", false},
		{"has-dash", true},
		{"has space", true},
		{"_under$core", false},
		{"123num", false},
	}
	for _, tc := range cases {
		got := needsQuoting(tc.input)
		if got != tc.want {
			t.Errorf("needsQuoting(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

// TestJsMethodName_PathParamSkipped verifies that path parameters like {name}
// are stripped when deriving the JS method name.
func TestJsMethodName_PathParamSkipped(t *testing.T) {
	r := Route{Method: "GET", Pattern: "/api/foo/{name}/bar"}
	got := jsMethodName(r, "foo")
	if got != "bar" {
		t.Errorf("jsMethodName() = %q, want %q", got, "bar")
	}
}

// TestEmitNamespace_SkipsDuplicateAndEmpty verifies that emitNamespace skips
// routes whose derived jsName is empty or duplicated.
func TestEmitNamespace_SkipsDuplicateAndEmpty(t *testing.T) {
	routes := []Route{
		{Method: "GET", Pattern: "/api/ns/foo", Name: "A"},
		{Method: "PUT", Pattern: "/api/ns/foo", Name: "B"}, // duplicate jsName "foo"
		{Method: "GET", Pattern: "/api/ns", Name: "C"},     // empty jsName (namespace root)
	}
	var b bytes.Buffer
	emitNamespace(&b, "ns", routes)
	out := b.String()
	// Should contain "foo" exactly once, and not contain an empty function name.
	if count := strings.Count(out, "foo: function"); count != 1 {
		t.Errorf("expected 1 'foo: function', got %d in:\n%s", count, out)
	}
}
