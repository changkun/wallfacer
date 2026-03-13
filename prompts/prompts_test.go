package prompts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"changkun.de/wallfacer/prompts"
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
