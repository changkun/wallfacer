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
