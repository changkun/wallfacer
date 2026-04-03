package prompts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/prompts"
)

// TestNewManager_OverrideRendered verifies that a valid override file in
// userDir is used in place of the embedded default.
func TestNewManager_OverrideRendered(t *testing.T) {
	dir := t.TempDir()
	const override = "Custom title prompt: {{.Prompt}}"
	if err := os.WriteFile(filepath.Join(dir, "title.tmpl"), []byte(override), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := prompts.NewManager(dir)
	got := mgr.Title("hello world")
	if got != "Custom title prompt: hello world" {
		t.Errorf("expected override content, got: %q", got)
	}
}

// TestNewManager_FallsBackToEmbedded verifies that a missing override falls
// back to the embedded template without error.
func TestNewManager_FallsBackToEmbedded(t *testing.T) {
	dir := t.TempDir()
	// No override files written.
	mgr := prompts.NewManager(dir)
	// Should produce non-empty output using the embedded title template.
	got := mgr.Title("my task prompt")
	if strings.TrimSpace(got) == "" {
		t.Error("expected non-empty output from embedded title template, got empty string")
	}
	// The embedded template must not contain the raw Go template syntax.
	if strings.Contains(got, "{{") {
		t.Errorf("embedded template was not rendered: %q", got)
	}
}

// TestNewManager_InvalidOverrideFallsBack verifies that if an override exists
// but fails to parse, the Manager silently falls back to the embedded default.
func TestNewManager_InvalidOverrideFallsBack(t *testing.T) {
	dir := t.TempDir()
	// Write an invalid template.
	if err := os.WriteFile(filepath.Join(dir, "title.tmpl"), []byte("{{broken"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := prompts.NewManager(dir)
	// Should fall back to the embedded template without panicking.
	got := mgr.Title("my task")
	if strings.TrimSpace(got) == "" {
		t.Error("expected non-empty fallback output, got empty string")
	}
}

// TestValidateTemplate_Valid verifies that a valid Go template passes validation.
func TestValidateTemplate_Valid(t *testing.T) {
	if err := prompts.ValidateTemplate("Hello {{.Name}}"); err != nil {
		t.Errorf("expected nil error for valid template, got: %v", err)
	}
}

// TestValidateTemplate_Invalid verifies that a broken Go template fails validation.
func TestValidateTemplate_Invalid(t *testing.T) {
	if err := prompts.ValidateTemplate("{{broken"); err == nil {
		t.Error("expected error for invalid template, got nil")
	}
}

// TestWriteOverride_Valid writes a valid override and reads it back.
func TestWriteOverride_Valid(t *testing.T) {
	dir := t.TempDir()
	mgr := prompts.NewManager(dir)

	const content = "Override: {{.Prompt}}"
	if err := mgr.WriteOverride("title", content); err != nil {
		t.Fatalf("WriteOverride: %v", err)
	}

	got, hasOverride, err := mgr.Content("title")
	if err != nil {
		t.Fatalf("Content: %v", err)
	}
	if !hasOverride {
		t.Error("expected hasOverride=true after writing")
	}
	if got != content {
		t.Errorf("Content = %q, want %q", got, content)
	}
}

// TestWriteOverride_Invalid returns an error (not nil) for an invalid template
// without writing anything to disk.
func TestWriteOverride_Invalid(t *testing.T) {
	dir := t.TempDir()
	mgr := prompts.NewManager(dir)

	if err := mgr.WriteOverride("title", "{{broken"); err == nil {
		t.Error("expected error for invalid template, got nil")
	}

	// Confirm no file was written.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("expected no files in dir, found: %v", entries)
	}
}

// TestWriteOverride_UnknownName returns an error for an unknown template name.
func TestWriteOverride_UnknownName(t *testing.T) {
	mgr := prompts.NewManager(t.TempDir())
	if err := mgr.WriteOverride("does_not_exist", "content"); err == nil {
		t.Error("expected error for unknown template name, got nil")
	}
}

// TestDeleteOverride removes an existing override.
func TestDeleteOverride_ExistingOverride(t *testing.T) {
	dir := t.TempDir()
	mgr := prompts.NewManager(dir)

	if err := mgr.WriteOverride("title", "custom {{.Prompt}}"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.DeleteOverride("title"); err != nil {
		t.Fatalf("DeleteOverride: %v", err)
	}
	_, hasOverride, err := mgr.Content("title")
	if err != nil {
		t.Fatalf("Content after delete: %v", err)
	}
	if hasOverride {
		t.Error("expected hasOverride=false after deleting override")
	}
}

// TestDeleteOverride_MissingReturnsErrNotExist returns an error when no
// override exists to delete.
func TestDeleteOverride_MissingReturnsErrNotExist(t *testing.T) {
	mgr := prompts.NewManager(t.TempDir())
	if err := mgr.DeleteOverride("title"); err == nil {
		t.Error("expected error when no override exists, got nil")
	}
}

// TestContent_AllKnownNames verifies that Content succeeds for all seven
// known template names and returns non-empty embedded defaults.
func TestContent_AllKnownNames(t *testing.T) {
	mgr := prompts.NewManager(t.TempDir())
	for _, name := range mgr.KnownNames() {
		content, hasOverride, err := mgr.Content(name)
		if err != nil {
			t.Errorf("Content(%q): unexpected error: %v", name, err)
			continue
		}
		if hasOverride {
			t.Errorf("Content(%q): unexpected override in clean temp dir", name)
		}
		if strings.TrimSpace(content) == "" {
			t.Errorf("Content(%q): embedded default is empty", name)
		}
	}
}

// TestContent_UnknownName returns an error for an unknown template API name.
func TestContent_UnknownName(t *testing.T) {
	mgr := prompts.NewManager(t.TempDir())
	_, _, err := mgr.Content("nonexistent")
	if err == nil {
		t.Error("expected error for unknown template name, got nil")
	}
}

// TestDefaultManagerRendersEmbedded verifies the package-level Default
// manager (empty userDir) still renders embedded templates correctly.
func TestDefaultManagerRendersEmbedded(t *testing.T) {
	got := prompts.Default.Title("test task")
	if strings.TrimSpace(got) == "" {
		t.Error("Default.Title returned empty string")
	}
}

// TestPromptsDir returns the configured directory.
func TestPromptsDir(t *testing.T) {
	dir := t.TempDir()
	mgr := prompts.NewManager(dir)
	if got := mgr.PromptsDir(); got != dir {
		t.Errorf("PromptsDir() = %q, want %q", got, dir)
	}
}

// TestPromptsDir_Empty returns empty string for Default manager.
func TestPromptsDir_Empty(t *testing.T) {
	if got := prompts.Default.PromptsDir(); got != "" {
		t.Errorf("Default.PromptsDir() = %q, want empty", got)
	}
}

// --- Validate ---

// TestValidate_ParseError verifies that Validate returns an error for a
// syntactically invalid template.
func TestValidate_ParseError(t *testing.T) {
	mgr := prompts.NewManager(t.TempDir())
	err := mgr.Validate("refinement", "{{.Unclosed")
	if err == nil {
		t.Fatal("expected error for unclosed template action, got nil")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("expected parse error, got: %v", err)
	}
}

// TestValidate_ExecutionError verifies that Validate returns an error when the
// template parses successfully but references a field that does not exist on
// the typed context struct.
func TestValidate_ExecutionError(t *testing.T) {
	mgr := prompts.NewManager(t.TempDir())
	// RefinementData has no field "FieldThatDoesNotExist".
	err := mgr.Validate("refinement", "{{.FieldThatDoesNotExist}}")
	if err == nil {
		t.Fatal("expected execution error for unknown field, got nil")
	}
	if !strings.Contains(err.Error(), "execution") {
		t.Errorf("expected execution error, got: %v", err)
	}
}

// TestValidate_ValidOverride verifies that Validate returns nil for a template
// that both parses and executes correctly against the known context.
func TestValidate_ValidOverride(t *testing.T) {
	mgr := prompts.NewManager(t.TempDir())
	if err := mgr.Validate("refinement", "Task: {{.Prompt}}"); err != nil {
		t.Errorf("expected nil for valid override, got: %v", err)
	}
}

// TestValidate_UnknownName verifies that Validate returns an error for a name
// that is not in the known template set.
func TestValidate_UnknownName(t *testing.T) {
	mgr := prompts.NewManager(t.TempDir())
	err := mgr.Validate("nonexistent", "some content")
	if err == nil {
		t.Fatal("expected error for unknown template name, got nil")
	}
	if !strings.Contains(err.Error(), "unknown template name") {
		t.Errorf("expected unknown-name error, got: %v", err)
	}
}

// TestValidate_AllKnownNamesWithEmbeddedDefaults verifies that the embedded
// default content passes Validate for every known template name. This acts as
// a regression guard ensuring embedded templates remain self-consistent.
func TestValidate_AllKnownNamesWithEmbeddedDefaults(t *testing.T) {
	mgr := prompts.NewManager(t.TempDir())
	for _, name := range mgr.KnownNames() {
		content, _, err := mgr.Content(name)
		if err != nil {
			t.Fatalf("Content(%q): %v", name, err)
		}
		if err := mgr.Validate(name, content); err != nil {
			t.Errorf("Validate(%q) on embedded default: %v", name, err)
		}
	}
}

// --- Tests for all six remaining template renderers ---

// TestRefinement_ReturnsNonEmptyRendered verifies that the refinement template
// renders with all fields populated and produces valid (non-empty, no raw
// template syntax) output.
func TestRefinement_ReturnsNonEmptyRendered(t *testing.T) {
	mgr := prompts.NewManager(t.TempDir())
	data := prompts.RefinementData{
		CreatedAt:        "2024-01-01",
		Today:            "2024-06-15",
		AgeDays:          165,
		Status:           "backlog",
		Prompt:           "build a login form",
		UserInstructions: "use TypeScript",
	}
	got := mgr.Refinement(data)
	if strings.TrimSpace(got) == "" {
		t.Error("Refinement returned empty string")
	}
	if strings.Contains(got, "{{") {
		t.Errorf("Refinement returned unreplaced template syntax: %q", got)
	}
}

// TestIdeation_ReturnsNonEmptyRendered verifies that the ideation template
// renders with existing tasks and categories populated.
func TestIdeation_ReturnsNonEmptyRendered(t *testing.T) {
	mgr := prompts.NewManager(t.TempDir())
	data := prompts.IdeationData{
		ExistingTasks: []prompts.IdeationTask{
			{Title: "existing task", Status: "done", Prompt: "do something"},
		},
		Categories: []string{"feature", "bugfix"},
	}
	got := mgr.Ideation(data)
	if strings.TrimSpace(got) == "" {
		t.Error("Ideation returned empty string")
	}
	if strings.Contains(got, "{{") {
		t.Errorf("Ideation returned unreplaced template syntax: %q", got)
	}
}

// TestOversight_ReturnsNonEmptyRendered verifies that the oversight template
// renders with a sample activity log.
func TestOversight_ReturnsNonEmptyRendered(t *testing.T) {
	mgr := prompts.NewManager(t.TempDir())
	got := mgr.Oversight("Phase 1: analyzed codebase. Phase 2: implemented feature.")
	if strings.TrimSpace(got) == "" {
		t.Error("Oversight returned empty string")
	}
	if strings.Contains(got, "{{") {
		t.Errorf("Oversight returned unreplaced template syntax: %q", got)
	}
}

// TestCommitMessage_ReturnsNonEmptyRendered verifies that the commit message
// template renders with prompt, diff stat, and recent log populated.
func TestCommitMessage_ReturnsNonEmptyRendered(t *testing.T) {
	mgr := prompts.NewManager(t.TempDir())
	data := prompts.CommitData{
		Prompt:    "add user authentication",
		DiffStat:  "3 files changed, 100 insertions(+), 2 deletions(-)",
		RecentLog: "abc1234 previous commit message",
	}
	got := mgr.CommitMessage(data)
	if strings.TrimSpace(got) == "" {
		t.Error("CommitMessage returned empty string")
	}
	if strings.Contains(got, "{{") {
		t.Errorf("CommitMessage returned unreplaced template syntax: %q", got)
	}
}

// TestConflictResolution_ReturnsNonEmptyRendered verifies that the conflict
// resolution template renders with container path and default branch populated.
func TestConflictResolution_ReturnsNonEmptyRendered(t *testing.T) {
	mgr := prompts.NewManager(t.TempDir())
	data := prompts.ConflictData{
		ContainerPath: "/workspace/myrepo",
		DefaultBranch: "main",
	}
	got := mgr.ConflictResolution(data)
	if strings.TrimSpace(got) == "" {
		t.Error("ConflictResolution returned empty string")
	}
	if strings.Contains(got, "{{") {
		t.Errorf("ConflictResolution returned unreplaced template syntax: %q", got)
	}
}

// TestTestVerification_ReturnsNonEmptyRendered verifies that the test
// verification template renders with all optional fields (criteria, impl
// result, diff) populated.
func TestTestVerification_ReturnsNonEmptyRendered(t *testing.T) {
	mgr := prompts.NewManager(t.TempDir())
	data := prompts.TestData{
		OriginalPrompt: "build a widget",
		Criteria:       "widget must render in < 100ms",
		ImplResult:     "I implemented the widget",
		Diff:           "--- a/widget.go\n+++ b/widget.go\n@@ -1 +1 @@",
	}
	got := mgr.TestVerification(data)
	if strings.TrimSpace(got) == "" {
		t.Error("TestVerification returned empty string")
	}
	if strings.Contains(got, "{{") {
		t.Errorf("TestVerification returned unreplaced template syntax: %q", got)
	}
}

// Package-level delegation functions — each verifies that the package-level
// convenience function delegates to Default and produces non-empty output.

func TestPackageLevelRefinement_NonEmpty(t *testing.T) {
	got := prompts.Refinement(prompts.RefinementData{Prompt: "do something", Status: "backlog"})
	if strings.TrimSpace(got) == "" {
		t.Error("prompts.Refinement() returned empty string")
	}
}

func TestPackageLevelIdeation_NonEmpty(t *testing.T) {
	got := prompts.Ideation(prompts.IdeationData{})
	if strings.TrimSpace(got) == "" {
		t.Error("prompts.Ideation() returned empty string")
	}
}

func TestPackageLevelOversight_NonEmpty(t *testing.T) {
	got := prompts.Oversight("some activity log")
	if strings.TrimSpace(got) == "" {
		t.Error("prompts.Oversight() returned empty string")
	}
}

func TestPackageLevelCommitMessage_NonEmpty(t *testing.T) {
	got := prompts.CommitMessage(prompts.CommitData{Prompt: "fix bug"})
	if strings.TrimSpace(got) == "" {
		t.Error("prompts.CommitMessage() returned empty string")
	}
}

func TestPackageLevelConflictResolution_NonEmpty(t *testing.T) {
	got := prompts.ConflictResolution(prompts.ConflictData{ContainerPath: "/workspace/repo", DefaultBranch: "main"})
	if strings.TrimSpace(got) == "" {
		t.Error("prompts.ConflictResolution() returned empty string")
	}
}

func TestPackageLevelTestVerification_NonEmpty(t *testing.T) {
	got := prompts.TestVerification(prompts.TestData{OriginalPrompt: "build widget"})
	if strings.TrimSpace(got) == "" {
		t.Error("prompts.TestVerification() returned empty string")
	}
}

func TestPackageLevelTitle_NonEmpty(t *testing.T) {
	got := prompts.Title("my task prompt")
	if strings.TrimSpace(got) == "" {
		t.Error("prompts.Title() returned empty string")
	}
}

// TestIdeation_EmptyData verifies that the ideation template renders
// successfully even when all data fields are zero-valued (no existing tasks,
// categories, or hotspots).
func TestIdeation_EmptyData(t *testing.T) {
	mgr := prompts.NewManager(t.TempDir())
	got := mgr.Ideation(prompts.IdeationData{})
	if strings.TrimSpace(got) == "" {
		t.Error("Ideation with empty data returned empty string")
	}
}

// TestTestVerification_NoCriteria verifies that the test verification template
// renders when only the required OriginalPrompt is set and all optional fields
// (Criteria, ImplResult, Diff) are empty.
func TestTestVerification_NoCriteria(t *testing.T) {
	mgr := prompts.NewManager(t.TempDir())
	got := mgr.TestVerification(prompts.TestData{OriginalPrompt: "build something"})
	if strings.TrimSpace(got) == "" {
		t.Error("TestVerification without criteria returned empty string")
	}
}

// TestRenderPlanning verifies that the planning template renders without error
// and contains key phrases.
func TestRenderPlanning(t *testing.T) {
	mgr := prompts.NewManager(t.TempDir())
	got := mgr.Planning()
	if strings.TrimSpace(got) == "" {
		t.Error("Planning returned empty string")
	}
	if strings.Contains(got, "{{") {
		t.Errorf("Planning returned unreplaced template syntax: %q", got)
	}
	for _, phrase := range []string{"spec", "planning", "specs/"} {
		if !strings.Contains(strings.ToLower(got), phrase) {
			t.Errorf("Planning output missing expected phrase %q", phrase)
		}
	}
}

func TestPackageLevelPlanning_NonEmpty(t *testing.T) {
	got := prompts.Planning()
	if strings.TrimSpace(got) == "" {
		t.Error("prompts.Planning() returned empty string")
	}
}
