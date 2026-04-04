package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpdateFrontmatter_SingleField(t *testing.T) {
	dir := t.TempDir()
	path := writeSpec(t, dir, "single.md", validSpec)

	err := UpdateFrontmatter(path, map[string]any{
		"status": "complete",
	})
	if err != nil {
		t.Fatalf("UpdateFrontmatter: %v", err)
	}

	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile after update: %v", err)
	}
	if s.Status != StatusComplete {
		t.Errorf("Status = %q, want %q", s.Status, StatusComplete)
	}
	// Body should be preserved.
	if !strings.Contains(s.Body, "This spec describes the sandbox backend interface.") {
		t.Errorf("Body was modified: %q", s.Body)
	}
}

func TestUpdateFrontmatter_MultipleFields(t *testing.T) {
	dir := t.TempDir()
	path := writeSpec(t, dir, "multi.md", validSpec)

	taskID := "550e8400-e29b-41d4-a716-446655440000"
	err := UpdateFrontmatter(path, map[string]any{
		"dispatched_task_id": &taskID,
		"updated":            time.Date(2026, 4, 4, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("UpdateFrontmatter: %v", err)
	}

	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile after update: %v", err)
	}
	if s.DispatchedTaskID == nil || *s.DispatchedTaskID != taskID {
		t.Errorf("DispatchedTaskID = %v, want %q", s.DispatchedTaskID, taskID)
	}
	if s.Updated.Year() != 2026 || s.Updated.Month() != 4 || s.Updated.Day() != 4 {
		t.Errorf("Updated = %v, want 2026-04-04", s.Updated.Time)
	}
}

func TestUpdateFrontmatter_NullValue(t *testing.T) {
	dir := t.TempDir()
	// Start with a spec that has a dispatched_task_id set.
	content := strings.Replace(validSpec, "dispatched_task_id: null", "dispatched_task_id: 550e8400-e29b-41d4-a716-446655440000", 1)
	path := writeSpec(t, dir, "null.md", content)

	// Verify it's set before clearing.
	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile before update: %v", err)
	}
	if s.DispatchedTaskID == nil {
		t.Fatal("DispatchedTaskID should be set before clearing")
	}

	// Clear it by setting to nil.
	err = UpdateFrontmatter(path, map[string]any{
		"dispatched_task_id": nil,
	})
	if err != nil {
		t.Fatalf("UpdateFrontmatter: %v", err)
	}

	s, err = ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile after update: %v", err)
	}
	if s.DispatchedTaskID != nil {
		t.Errorf("DispatchedTaskID = %v, want nil", s.DispatchedTaskID)
	}
}

func TestUpdateFrontmatter_NilStringPointer(t *testing.T) {
	dir := t.TempDir()
	content := strings.Replace(validSpec, "dispatched_task_id: null", "dispatched_task_id: some-id", 1)
	path := writeSpec(t, dir, "nilptr.md", content)

	// Clear using typed nil *string.
	err := UpdateFrontmatter(path, map[string]any{
		"dispatched_task_id": (*string)(nil),
	})
	if err != nil {
		t.Fatalf("UpdateFrontmatter: %v", err)
	}

	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile after update: %v", err)
	}
	if s.DispatchedTaskID != nil {
		t.Errorf("DispatchedTaskID = %v, want nil", s.DispatchedTaskID)
	}
}

func TestUpdateFrontmatter_PreservesBody(t *testing.T) {
	dir := t.TempDir()
	// Body contains --- which could trip up naive parsing.
	content := `---
title: Tricky Body
status: drafted
depends_on: []
affects:
  - internal/spec/
effort: small
created: 2026-01-01
updated: 2026-01-01
author: changkun
dispatched_task_id: null
---

# Tricky Body

Some content with code blocks:

` + "```yaml\n---\nkey: value\n---\n```" + `

And more content after.
`
	path := writeSpec(t, dir, "tricky.md", content)

	err := UpdateFrontmatter(path, map[string]any{
		"status": "validated",
	})
	if err != nil {
		t.Fatalf("UpdateFrontmatter: %v", err)
	}

	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile after update: %v", err)
	}
	if s.Status != StatusValidated {
		t.Errorf("Status = %q, want %q", s.Status, StatusValidated)
	}
	if !strings.Contains(s.Body, "```yaml\n---\nkey: value\n---\n```") {
		t.Errorf("Body code block was corrupted: %q", s.Body)
	}
	if !strings.Contains(s.Body, "And more content after.") {
		t.Errorf("Body trailing content was lost: %q", s.Body)
	}
}

func TestUpdateFrontmatter_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := writeSpec(t, dir, "roundtrip.md", validSpec)

	// Parse original.
	orig, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile original: %v", err)
	}

	// Update one field.
	err = UpdateFrontmatter(path, map[string]any{
		"status": "complete",
	})
	if err != nil {
		t.Fatalf("UpdateFrontmatter: %v", err)
	}

	// Parse updated.
	updated, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile updated: %v", err)
	}

	// Verify the updated field changed.
	if updated.Status != StatusComplete {
		t.Errorf("Status = %q, want %q", updated.Status, StatusComplete)
	}

	// Verify all other fields are unchanged.
	if updated.Title != orig.Title {
		t.Errorf("Title changed: %q → %q", orig.Title, updated.Title)
	}
	if len(updated.DependsOn) != len(orig.DependsOn) {
		t.Errorf("DependsOn changed: %v → %v", orig.DependsOn, updated.DependsOn)
	}
	if len(updated.Affects) != len(orig.Affects) {
		t.Errorf("Affects changed: %v → %v", orig.Affects, updated.Affects)
	}
	if updated.Effort != orig.Effort {
		t.Errorf("Effort changed: %q → %q", orig.Effort, updated.Effort)
	}
	if !updated.Created.Equal(orig.Created.Time) {
		t.Errorf("Created changed: %v → %v", orig.Created.Time, updated.Created.Time)
	}
	if updated.Author != orig.Author {
		t.Errorf("Author changed: %q → %q", orig.Author, updated.Author)
	}
	if updated.Body != orig.Body {
		t.Errorf("Body changed: %q → %q", orig.Body, updated.Body)
	}
}

func TestUpdateFrontmatter_NonexistentFile(t *testing.T) {
	err := UpdateFrontmatter("/nonexistent/path/spec.md", map[string]any{
		"status": "complete",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestUpdateFrontmatter_InvalidFrontmatter(t *testing.T) {
	dir := t.TempDir()
	content := "---\n: invalid: yaml: [[\n---\n\nBody.\n"
	path := writeSpec(t, dir, "invalid.md", content)

	err := UpdateFrontmatter(path, map[string]any{
		"status": "complete",
	})
	if err == nil {
		t.Fatal("expected error for invalid frontmatter")
	}
}

func TestUpdateFrontmatter_EmptyUpdates(t *testing.T) {
	dir := t.TempDir()
	path := writeSpec(t, dir, "noop.md", validSpec)

	// Read original content.
	orig, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read original: %v", err)
	}

	err = UpdateFrontmatter(path, map[string]any{})
	if err != nil {
		t.Fatalf("UpdateFrontmatter with empty updates: %v", err)
	}

	// File should be unchanged.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(after) != string(orig) {
		t.Error("file was modified despite empty updates")
	}
}

func TestUpdateFrontmatter_DateType(t *testing.T) {
	dir := t.TempDir()
	path := writeSpec(t, dir, "date.md", validSpec)

	d := Date{Time: time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)}
	err := UpdateFrontmatter(path, map[string]any{
		"updated": d,
	})
	if err != nil {
		t.Fatalf("UpdateFrontmatter: %v", err)
	}

	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile after update: %v", err)
	}
	if s.Updated.Year() != 2026 || s.Updated.Month() != 5 || s.Updated.Day() != 15 {
		t.Errorf("Updated = %v, want 2026-05-15", s.Updated.Time)
	}
}

func TestUpdateFrontmatter_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := writeSpec(t, dir, "atomic.md", validSpec)

	err := UpdateFrontmatter(path, map[string]any{
		"status": "complete",
	})
	if err != nil {
		t.Fatalf("UpdateFrontmatter: %v", err)
	}

	// Verify no temp files left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".spec-update-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}

	// Verify file permissions are preserved.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0644 {
		t.Errorf("file permissions = %o, want 0644", info.Mode().Perm())
	}
}

func TestUpdateFrontmatter_AppendNewKey(t *testing.T) {
	dir := t.TempDir()
	// Spec without dispatched_task_id field at all.
	content := `---
title: Minimal
status: drafted
depends_on: []
affects: []
effort: small
created: 2026-01-01
updated: 2026-01-01
author: changkun
---

# Minimal Spec
`
	path := writeSpec(t, dir, "minimal.md", content)

	taskID := "new-task-id"
	err := UpdateFrontmatter(path, map[string]any{
		"dispatched_task_id": &taskID,
	})
	if err != nil {
		t.Fatalf("UpdateFrontmatter: %v", err)
	}

	// The new field should be readable.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "dispatched_task_id") {
		t.Error("new key was not appended to frontmatter")
	}

	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile after append: %v", err)
	}
	if s.DispatchedTaskID == nil || *s.DispatchedTaskID != taskID {
		t.Errorf("DispatchedTaskID = %v, want %q", s.DispatchedTaskID, taskID)
	}
	if s.Body != "# Minimal Spec\n" {
		t.Errorf("Body = %q, want %q", s.Body, "# Minimal Spec\n")
	}
}

func TestUpdateFrontmatter_PreservesFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "perms.md")
	if err := os.WriteFile(path, []byte(validSpec), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := UpdateFrontmatter(path, map[string]any{"status": "complete"})
	if err != nil {
		t.Fatalf("UpdateFrontmatter: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("permissions = %o, want 0600", info.Mode().Perm())
	}
}
