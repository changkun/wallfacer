package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrackDisplayName(t *testing.T) {
	cases := map[string]string{
		"local":        "Local Product",
		"foundations":  "Foundations",
		"cloud":        "Cloud Platform",
		"shared":       "Shared Design",
		"custom-track": "Custom Track",
		"single":       "Single",
	}
	for in, want := range cases {
		if got := TrackDisplayName(in); got != want {
			t.Errorf("TrackDisplayName(%q) = %q, want %q", in, got, want)
		}
	}
}

func readReadme(t *testing.T, ws string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(ws, "specs", "README.md"))
	if err != nil {
		t.Fatalf("read readme: %v", err)
	}
	return string(data)
}

func TestEnsureReadme_CreatesWhenMissing(t *testing.T) {
	ws := t.TempDir()
	meta := Meta{
		Path:    "specs/local/auth-refactor.md",
		Title:   "Auth Refactor",
		Status:  StatusDrafted,
		Summary: "Rework session tokens to meet legal requirements.",
	}
	if err := EnsureReadme(ws, meta); err != nil {
		t.Fatalf("EnsureReadme: %v", err)
	}
	got := readReadme(t, ws)
	if !strings.Contains(got, "# Specs") {
		t.Errorf("missing H1 title: %s", got)
	}
	if !strings.Contains(got, "## Local Product") {
		t.Errorf("missing track heading: %s", got)
	}
	if !strings.Contains(got, "| [auth-refactor.md](local/auth-refactor.md) | Drafted | Rework session tokens to meet legal requirements. |") {
		t.Errorf("row mismatch, got: %s", got)
	}
}

func TestEnsureReadme_AppendsWhenExists(t *testing.T) {
	ws := t.TempDir()
	readmePath := filepath.Join(ws, "specs", "README.md")
	if err := os.MkdirAll(filepath.Dir(readmePath), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `# Specs

Project roadmap.

## Local Product

Desktop experience.

| Spec | Status | Delivers |
|------|--------|----------|
| [existing.md](local/existing.md) | **Complete** | First entry |
`
	if err := os.WriteFile(readmePath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	meta := Meta{
		Path:    "specs/local/new-feature.md",
		Status:  StatusVague,
		Summary: "Second entry",
	}
	if err := EnsureReadme(ws, meta); err != nil {
		t.Fatalf("EnsureReadme: %v", err)
	}
	got := readReadme(t, ws)
	// First row preserved verbatim.
	if !strings.Contains(got, "| [existing.md](local/existing.md) | **Complete** | First entry |") {
		t.Errorf("existing row lost: %s", got)
	}
	// New row appended.
	if !strings.Contains(got, "| [new-feature.md](local/new-feature.md) | Vague | Second entry |") {
		t.Errorf("new row missing: %s", got)
	}
	// Order: new row comes after the existing row.
	existingIdx := strings.Index(got, "[existing.md]")
	newIdx := strings.Index(got, "[new-feature.md]")
	if existingIdx < 0 || newIdx < 0 || existingIdx > newIdx {
		t.Errorf("ordering wrong (existing=%d, new=%d)", existingIdx, newIdx)
	}
}

func TestEnsureReadme_PreservesUserContent(t *testing.T) {
	ws := t.TempDir()
	readmePath := filepath.Join(ws, "specs", "README.md")
	if err := os.MkdirAll(filepath.Dir(readmePath), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `# Specs

Intro paragraph — do not touch.

![diagram](./diagram.png)

## Local Product

Desktop. This paragraph must survive.

| Spec | Status | Delivers |
|------|--------|----------|
| [a.md](local/a.md) | Drafted | Alpha |

### Notes

Freeform markdown after the table.

## Foundations

No table in this section yet.

## Archive

Trailing section.
`
	if err := os.WriteFile(readmePath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := Meta{
		Path:    "specs/local/b.md",
		Status:  StatusValidated,
		Summary: "Bravo",
	}
	if err := EnsureReadme(ws, meta); err != nil {
		t.Fatalf("EnsureReadme: %v", err)
	}
	got := readReadme(t, ws)
	// Every user-authored section still present.
	for _, needle := range []string{
		"Intro paragraph — do not touch.",
		"![diagram](./diagram.png)",
		"Desktop. This paragraph must survive.",
		"### Notes",
		"Freeform markdown after the table.",
		"## Foundations",
		"No table in this section yet.",
		"## Archive",
		"Trailing section.",
		"| [a.md](local/a.md) | Drafted | Alpha |",
	} {
		if !strings.Contains(got, needle) {
			t.Errorf("missing user content %q in:\n%s", needle, got)
		}
	}
	// New row inserted into Local Product table only, not elsewhere.
	if !strings.Contains(got, "| [b.md](local/b.md) | Validated | Bravo |") {
		t.Errorf("new row missing: %s", got)
	}
	// Foundations heading still has no table.
	foundIdx := strings.Index(got, "## Foundations")
	archiveIdx := strings.Index(got, "## Archive")
	if foundIdx < 0 || archiveIdx < 0 {
		t.Fatalf("section markers missing: %s", got)
	}
	archive := got[foundIdx:archiveIdx]
	if strings.Contains(archive, "| Spec |") {
		t.Errorf("Foundations section unexpectedly gained a table:\n%s", archive)
	}
}

func TestEnsureReadme_AddsNewTrackSection(t *testing.T) {
	ws := t.TempDir()
	readmePath := filepath.Join(ws, "specs", "README.md")
	if err := os.MkdirAll(filepath.Dir(readmePath), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `# Specs

## Local Product

| Spec | Status | Delivers |
|------|--------|----------|
| [a.md](local/a.md) | Drafted | Alpha |
`
	if err := os.WriteFile(readmePath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := Meta{
		Path:    "specs/foundations/core.md",
		Status:  StatusVague,
		Summary: "Core interfaces",
	}
	if err := EnsureReadme(ws, meta); err != nil {
		t.Fatalf("EnsureReadme: %v", err)
	}
	got := readReadme(t, ws)
	// Existing section intact.
	if !strings.Contains(got, "| [a.md](local/a.md) | Drafted | Alpha |") {
		t.Errorf("existing row lost: %s", got)
	}
	// New section present at the end.
	if !strings.Contains(got, "## Foundations") {
		t.Errorf("Foundations heading not added: %s", got)
	}
	if !strings.Contains(got, "| [core.md](foundations/core.md) | Vague | Core interfaces |") {
		t.Errorf("new row missing: %s", got)
	}
	// "## Foundations" sits after "## Local Product".
	localIdx := strings.Index(got, "## Local Product")
	foundIdx := strings.Index(got, "## Foundations")
	if foundIdx < localIdx {
		t.Errorf("section order wrong (local=%d, foundations=%d)", localIdx, foundIdx)
	}
}

func TestEnsureReadme_HeadingWithoutTableGetsTable(t *testing.T) {
	ws := t.TempDir()
	readmePath := filepath.Join(ws, "specs", "README.md")
	if err := os.MkdirAll(filepath.Dir(readmePath), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `# Specs

## Local Product

Desktop product specs go here.

Another paragraph.

## Archive

Done specs.
`
	if err := os.WriteFile(readmePath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := Meta{Path: "specs/local/first.md", Status: StatusDrafted, Summary: "First"}
	if err := EnsureReadme(ws, meta); err != nil {
		t.Fatalf("EnsureReadme: %v", err)
	}
	got := readReadme(t, ws)
	if !strings.Contains(got, "| [first.md](local/first.md) | Drafted | First |") {
		t.Errorf("row missing: %s", got)
	}
	// Table was inserted inside the Local Product section, not into
	// Archive.
	localStart := strings.Index(got, "## Local Product")
	archiveStart := strings.Index(got, "## Archive")
	localBlock := got[localStart:archiveStart]
	if !strings.Contains(localBlock, "| Spec | Status | Delivers |") {
		t.Errorf("table not inserted into Local Product block:\n%s", localBlock)
	}
	if strings.Contains(got[archiveStart:], "| Spec |") {
		t.Errorf("table leaked into Archive section: %s", got[archiveStart:])
	}
}

func TestEnsureReadme_SummaryPlaceholderWhenEmpty(t *testing.T) {
	ws := t.TempDir()
	meta := Meta{Path: "specs/local/x.md", Status: StatusDrafted}
	if err := EnsureReadme(ws, meta); err != nil {
		t.Fatalf("EnsureReadme: %v", err)
	}
	got := readReadme(t, ws)
	if !strings.Contains(got, "(agent will fill this in)") {
		t.Errorf("placeholder missing: %s", got)
	}
}

func TestEnsureReadme_SummaryPipesEscaped(t *testing.T) {
	ws := t.TempDir()
	meta := Meta{
		Path:    "specs/local/x.md",
		Status:  StatusDrafted,
		Summary: "use | pipes | safely",
	}
	if err := EnsureReadme(ws, meta); err != nil {
		t.Fatalf("EnsureReadme: %v", err)
	}
	got := readReadme(t, ws)
	if !strings.Contains(got, `use \| pipes \| safely`) {
		t.Errorf("pipes not escaped: %s", got)
	}
}

func TestEnsureReadme_AtomicWriteDoesNotLoseContentOnRenameFailure(t *testing.T) {
	// Simulate a rename failure by making the parent directory
	// read-only AFTER the initial file is in place. On most POSIX
	// systems this prevents rename from replacing the file, so the
	// original should remain untouched.
	if os.Getuid() == 0 {
		t.Skip("skipping atomic-write failure simulation as root")
	}
	ws := t.TempDir()
	readmePath := filepath.Join(ws, "specs", "README.md")
	if err := os.MkdirAll(filepath.Dir(readmePath), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := "# Pristine\n\nUntouched.\n"
	if err := os.WriteFile(readmePath, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(readmePath), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(filepath.Dir(readmePath), 0o755)
	})
	_ = EnsureReadme(ws, Meta{Path: "specs/local/x.md", Status: StatusDrafted, Summary: "X"})
	// Restore permissions so we can read the file back.
	if err := os.Chmod(filepath.Dir(readmePath), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read original: %v", err)
	}
	if string(got) != orig {
		t.Errorf("original content mutated on failure:\n got %q\nwant %q", got, orig)
	}
}

func TestEnsureReadme_RejectsInvalidPath(t *testing.T) {
	ws := t.TempDir()
	meta := Meta{Path: "foo/bar.md"} // not under specs/<track>/
	if err := EnsureReadme(ws, meta); err == nil {
		t.Fatalf("expected error for invalid path, got nil")
	}
}
