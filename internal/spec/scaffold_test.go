package spec

import (
	"os"
	"strings"
	"testing"
	"time"
)

// fixedNow is a deterministic timestamp used across scaffold tests so
// rendered output is stable and independent of the host clock.
var fixedNow = time.Date(2026, 4, 12, 10, 0, 0, 0, time.UTC)

// scaffoldChdir parks the test in a temp directory so Scaffold's
// repo-relative path contract (`specs/<track>/...`) resolves to the
// temp cwd. Returns the absolute temp path; restores the prior cwd
// via t.Cleanup.
func scaffoldChdir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	return dir
}

func TestScaffold_HappyPath(t *testing.T) {
	scaffoldChdir(t)
	path := "specs/local/foo.md"

	got, err := Scaffold(ScaffoldOptions{
		Path:   path,
		Title:  "Foo",
		Status: StatusDrafted,
		Effort: EffortLarge,
		Author: "Tester",
		Now:    fixedNow,
	})
	if err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	if got != path {
		t.Errorf("returned path = %q, want %q", got, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read created file: %v", err)
	}
	body := string(data)

	for _, want := range []string{
		"title: Foo",
		"status: drafted",
		"effort: large",
		"author: Tester",
		"created: 2026-04-12",
		"updated: 2026-04-12",
		"depends_on: []",
		"dispatched_task_id: null",
		"# Foo",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\nfull:\n%s", want, body)
		}
	}
}

func TestScaffold_DefaultsTitleFromBasename(t *testing.T) {
	scaffoldChdir(t)
	path := "specs/local/foo-bar.md"

	if _, err := Scaffold(ScaffoldOptions{Path: path, Now: fixedNow}); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "title: Foo Bar\n") {
		t.Errorf("expected title: Foo Bar in output, got:\n%s", data)
	}
}

func TestScaffold_DefaultsStatusAndEffort(t *testing.T) {
	scaffoldChdir(t)
	path := "specs/local/defaults.md"

	if _, err := Scaffold(ScaffoldOptions{Path: path, Now: fixedNow}); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	data, _ := os.ReadFile(path)
	body := string(data)
	if !strings.Contains(body, "status: vague\n") {
		t.Errorf("expected default status=vague, got:\n%s", body)
	}
	if !strings.Contains(body, "effort: medium\n") {
		t.Errorf("expected default effort=medium, got:\n%s", body)
	}
}

func TestScaffold_DefaultsAuthor(t *testing.T) {
	// ResolveAuthor reads from git config and falls back to "unknown".
	// Either is fine — the contract is that the author line is never
	// left blank.
	scaffoldChdir(t)
	path := "specs/local/noauthor.md"

	if _, err := Scaffold(ScaffoldOptions{Path: path, Now: fixedNow}); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	data, _ := os.ReadFile(path)
	// Find the author line and assert it's non-empty.
	for line := range strings.SplitSeq(string(data), "\n") {
		if val, ok := strings.CutPrefix(line, "author: "); ok {
			if strings.TrimSpace(val) == "" {
				t.Errorf("author line is blank")
			}
			return
		}
	}
	t.Errorf("no author line found in output")
}

func TestScaffold_RejectsInvalidStatus(t *testing.T) {
	scaffoldChdir(t)
	path := "specs/local/bad.md"
	_, err := Scaffold(ScaffoldOptions{
		Path:   path,
		Status: Status("frobnicated"),
		Now:    fixedNow,
	})
	if err == nil {
		t.Error("expected error for invalid status, got nil")
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Error("file should not be created when validation fails")
	}
}

func TestScaffold_RejectsInvalidEffort(t *testing.T) {
	scaffoldChdir(t)
	_, err := Scaffold(ScaffoldOptions{
		Path:   "specs/local/bad.md",
		Effort: Effort("huge"),
		Now:    fixedNow,
	})
	if err == nil {
		t.Error("expected error for invalid effort, got nil")
	}
}

func TestScaffold_RejectsPathOutsideSpecs(t *testing.T) {
	scaffoldChdir(t)
	cases := []string{
		"other/foo.md",              // not under specs/
		"specs/foo.md",              // missing track directory
		"/tmp/abs/specs/local/x.md", // absolute path — validator expects repo-relative
	}
	for _, p := range cases {
		_, err := Scaffold(ScaffoldOptions{Path: p, Now: fixedNow})
		if err == nil {
			t.Errorf("Scaffold(%q) expected error, got nil", p)
		}
	}
}

func TestScaffold_RejectsNonMarkdown(t *testing.T) {
	scaffoldChdir(t)
	_, err := Scaffold(ScaffoldOptions{Path: "specs/local/foo.txt", Now: fixedNow})
	if err == nil {
		t.Error("expected error for non-.md path, got nil")
	}
}

func TestScaffold_RejectsExistingFileWithoutForce(t *testing.T) {
	scaffoldChdir(t)
	path := "specs/local/exists.md"

	if _, err := Scaffold(ScaffoldOptions{Path: path, Now: fixedNow}); err != nil {
		t.Fatalf("first Scaffold: %v", err)
	}
	_, err := Scaffold(ScaffoldOptions{Path: path, Now: fixedNow})
	if err == nil {
		t.Error("second Scaffold should error without Force")
	}
}

func TestScaffold_ForceOverwrites(t *testing.T) {
	scaffoldChdir(t)
	path := "specs/local/overwrite.md"

	if _, err := Scaffold(ScaffoldOptions{Path: path, Title: "First", Now: fixedNow}); err != nil {
		t.Fatalf("first Scaffold: %v", err)
	}
	if _, err := Scaffold(ScaffoldOptions{Path: path, Title: "Second", Force: true, Now: fixedNow}); err != nil {
		t.Fatalf("force Scaffold: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "title: Second\n") {
		t.Errorf("expected overwritten title, got:\n%s", data)
	}
}

func TestScaffold_CreatesParentDirectory(t *testing.T) {
	scaffoldChdir(t)
	path := "specs/local/auth/subfolder/deep.md"

	if _, err := Scaffold(ScaffoldOptions{Path: path, Now: fixedNow}); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file to exist at %s: %v", path, err)
	}
}

func TestScaffold_DependsOnList(t *testing.T) {
	scaffoldChdir(t)
	path := "specs/local/deps.md"

	_, err := Scaffold(ScaffoldOptions{
		Path:      path,
		DependsOn: []string{"specs/foundations/a.md", "specs/foundations/b.md"},
		Now:       fixedNow,
	})
	if err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	data, _ := os.ReadFile(path)
	body := string(data)
	if !strings.Contains(body, "depends_on:\n  - specs/foundations/a.md\n  - specs/foundations/b.md\n") {
		t.Errorf("expected depends_on YAML list, got:\n%s", body)
	}
	if strings.Contains(body, "depends_on: []") {
		t.Errorf("depends_on should not be rendered as empty list when DependsOn is set")
	}
}

// TestScaffold_ValidatesViaSpecValidate is the contract test: the
// scaffold output, parsed back via ParseFile + ValidateSpec, must pass
// with zero errors. Guards against drift between the skeleton template
// and the validator.
func TestScaffold_ValidatesViaSpecValidate(t *testing.T) {
	dir := scaffoldChdir(t)
	path := "specs/local/roundtrip.md"

	if _, err := Scaffold(ScaffoldOptions{
		Path:   path,
		Title:  "Round Trip",
		Status: StatusDrafted,
		Effort: EffortMedium,
		Author: "Tester",
		Now:    fixedNow,
	}); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	results := ValidateSpec(s, dir, true)
	for _, r := range results {
		if r.Severity == SeverityError {
			t.Errorf("validation error %q: %s", r.Rule, r.Message)
		}
	}
}

// --- moved from internal/cli/spec_test.go ---

func TestTitleFromFilename(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"specs/local/my-feature.md", "My Feature"},
		{"foo.md", "Foo"},
		{"my_snake_case.md", "My Snake Case"},
		{"mixed-under_score.md", "Mixed Under Score"},
		{"Already-Titled.md", "Already Titled"},
		{"singleword.md", "Singleword"},
	}
	for _, c := range cases {
		if got := TitleFromFilename(c.in); got != c.want {
			t.Errorf("TitleFromFilename(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidateSpecPath(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"specs/local/foo.md", false},
		{"specs/local/sub/foo.md", false},
		{"specs/foundations/backend.md", false},
		{"foo.md", true},              // not under specs/
		{"specs/foo.md", true},        // missing track directory
		{"specs/local/foo.txt", true}, // wrong extension
		{"specs/local/foo", true},     // no extension
		{"specs//foo.md", true},       // empty track
	}
	for _, c := range cases {
		err := ValidateSpecPath(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("ValidateSpecPath(%q) err=%v, wantErr=%v", c.in, err, c.wantErr)
		}
	}
}

func TestRenderSkeleton_FrontmatterShape(t *testing.T) {
	got := RenderSkeleton("My Title", StatusVague, EffortMedium, "Test User", nil, fixedNow)

	wantLines := []string{
		"---",
		"title: My Title",
		"status: vague",
		"depends_on: []",
		"affects: []",
		"effort: medium",
		"created: 2026-04-12",
		"updated: 2026-04-12",
		"author: Test User",
		"dispatched_task_id: null",
		"---",
	}
	for _, line := range wantLines {
		if !strings.Contains(got, line+"\n") {
			t.Errorf("skeleton missing frontmatter line %q\nfull output:\n%s", line, got)
		}
	}
	if !strings.Contains(got, "# My Title\n") {
		t.Errorf("skeleton missing body heading")
	}
}
