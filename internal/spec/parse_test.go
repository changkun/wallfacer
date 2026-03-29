package spec

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSpec(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write test spec: %v", err)
	}
	return path
}

const validSpec = `---
title: Sandbox Backends
status: validated
track: foundations
depends_on:
  - specs/foundations/storage-backends.md
affects:
  - internal/sandbox/
  - internal/runner/execute.go
effort: large
created: 2026-01-15
updated: 2026-03-28
author: changkun
dispatched_task_id: null
---

# Sandbox Backends

This spec describes the sandbox backend interface.
`

func TestParseFile_ValidSpec(t *testing.T) {
	dir := t.TempDir()
	path := writeSpec(t, dir, "sandbox-backends.md", validSpec)

	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if s.Title != "Sandbox Backends" {
		t.Errorf("Title = %q, want %q", s.Title, "Sandbox Backends")
	}
	if s.Status != StatusValidated {
		t.Errorf("Status = %q, want %q", s.Status, StatusValidated)
	}
	if s.Track != TrackFoundations {
		t.Errorf("Track = %q, want %q", s.Track, TrackFoundations)
	}
	if len(s.DependsOn) != 1 || s.DependsOn[0] != "specs/foundations/storage-backends.md" {
		t.Errorf("DependsOn = %v, want [specs/foundations/storage-backends.md]", s.DependsOn)
	}
	if len(s.Affects) != 2 {
		t.Errorf("Affects = %v, want 2 entries", s.Affects)
	}
	if s.Effort != EffortLarge {
		t.Errorf("Effort = %q, want %q", s.Effort, EffortLarge)
	}
	if s.Created.Year() != 2026 || s.Created.Month() != 1 || s.Created.Day() != 15 {
		t.Errorf("Created = %v, want 2026-01-15", s.Created.Time)
	}
	if s.Updated.Year() != 2026 || s.Updated.Month() != 3 || s.Updated.Day() != 28 {
		t.Errorf("Updated = %v, want 2026-03-28", s.Updated.Time)
	}
	if s.Author != "changkun" {
		t.Errorf("Author = %q, want %q", s.Author, "changkun")
	}
	if s.DispatchedTaskID != nil {
		t.Errorf("DispatchedTaskID = %v, want nil", s.DispatchedTaskID)
	}
	if s.Path != path {
		t.Errorf("Path = %q, want %q", s.Path, path)
	}
}

func TestParseFile_AllStatuses(t *testing.T) {
	statuses := []Status{StatusVague, StatusDrafted, StatusValidated, StatusComplete, StatusStale}
	dir := t.TempDir()

	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			content := "---\ntitle: Test\nstatus: " + string(status) + "\ntrack: local\neffort: small\ncreated: 2026-01-01\nupdated: 2026-01-01\nauthor: test\ndispatched_task_id: null\n---\n"
			path := writeSpec(t, dir, "status-"+string(status)+".md", content)
			s, err := ParseFile(path)
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}
			if s.Status != status {
				t.Errorf("Status = %q, want %q", s.Status, status)
			}
		})
	}
}

func TestParseFile_AllTracks(t *testing.T) {
	tracks := []Track{TrackFoundations, TrackLocal, TrackCloud, TrackShared}
	dir := t.TempDir()

	for _, track := range tracks {
		t.Run(string(track), func(t *testing.T) {
			content := "---\ntitle: Test\nstatus: drafted\ntrack: " + string(track) + "\neffort: small\ncreated: 2026-01-01\nupdated: 2026-01-01\nauthor: test\ndispatched_task_id: null\n---\n"
			path := writeSpec(t, dir, "track-"+string(track)+".md", content)
			s, err := ParseFile(path)
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}
			if s.Track != track {
				t.Errorf("Track = %q, want %q", s.Track, track)
			}
		})
	}
}

func TestParseFile_AllEfforts(t *testing.T) {
	efforts := []Effort{EffortSmall, EffortMedium, EffortLarge, EffortXLarge}
	dir := t.TempDir()

	for _, effort := range efforts {
		t.Run(string(effort), func(t *testing.T) {
			content := "---\ntitle: Test\nstatus: drafted\ntrack: local\neffort: " + string(effort) + "\ncreated: 2026-01-01\nupdated: 2026-01-01\nauthor: test\ndispatched_task_id: null\n---\n"
			path := writeSpec(t, dir, "effort-"+string(effort)+".md", content)
			s, err := ParseFile(path)
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}
			if s.Effort != effort {
				t.Errorf("Effort = %q, want %q", s.Effort, effort)
			}
		})
	}
}

func TestParseFile_NullDispatchID(t *testing.T) {
	dir := t.TempDir()
	content := "---\ntitle: Test\nstatus: drafted\ntrack: local\neffort: small\ncreated: 2026-01-01\nupdated: 2026-01-01\nauthor: test\ndispatched_task_id: null\n---\n"
	path := writeSpec(t, dir, "null-dispatch.md", content)

	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if s.DispatchedTaskID != nil {
		t.Errorf("DispatchedTaskID = %v, want nil", s.DispatchedTaskID)
	}
}

func TestParseFile_UUIDDispatchID(t *testing.T) {
	dir := t.TempDir()
	content := "---\ntitle: Test\nstatus: drafted\ntrack: local\neffort: small\ncreated: 2026-01-01\nupdated: 2026-01-01\nauthor: test\ndispatched_task_id: 550e8400-e29b-41d4-a716-446655440000\n---\n"
	path := writeSpec(t, dir, "uuid-dispatch.md", content)

	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if s.DispatchedTaskID == nil {
		t.Fatal("DispatchedTaskID is nil, want UUID string")
	}
	if *s.DispatchedTaskID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("DispatchedTaskID = %q, want %q", *s.DispatchedTaskID, "550e8400-e29b-41d4-a716-446655440000")
	}
}

func TestParseFile_EmptyDependsOn(t *testing.T) {
	dir := t.TempDir()
	content := "---\ntitle: Test\nstatus: drafted\ntrack: local\ndepends_on: []\neffort: small\ncreated: 2026-01-01\nupdated: 2026-01-01\nauthor: test\ndispatched_task_id: null\n---\n"
	path := writeSpec(t, dir, "empty-deps.md", content)

	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if s.DependsOn == nil {
		t.Fatal("DependsOn is nil, want empty slice")
	}
	if len(s.DependsOn) != 0 {
		t.Errorf("DependsOn has %d entries, want 0", len(s.DependsOn))
	}
}

func TestParseFile_MultipleDependsOn(t *testing.T) {
	dir := t.TempDir()
	content := "---\ntitle: Test\nstatus: drafted\ntrack: local\ndepends_on:\n  - specs/a.md\n  - specs/b.md\n  - specs/c.md\neffort: small\ncreated: 2026-01-01\nupdated: 2026-01-01\nauthor: test\ndispatched_task_id: null\n---\n"
	path := writeSpec(t, dir, "multi-deps.md", content)

	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(s.DependsOn) != 3 {
		t.Fatalf("DependsOn has %d entries, want 3", len(s.DependsOn))
	}
	want := []string{"specs/a.md", "specs/b.md", "specs/c.md"}
	for i, w := range want {
		if s.DependsOn[i] != w {
			t.Errorf("DependsOn[%d] = %q, want %q", i, s.DependsOn[i], w)
		}
	}
}

func TestParseFile_BodyExtraction(t *testing.T) {
	dir := t.TempDir()
	content := "---\ntitle: Test\nstatus: drafted\ntrack: local\neffort: small\ncreated: 2026-01-01\nupdated: 2026-01-01\nauthor: test\ndispatched_task_id: null\n---\n\n# Hello World\n\nSome body content.\n"
	path := writeSpec(t, dir, "with-body.md", content)

	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if s.Body != "# Hello World\n\nSome body content.\n" {
		t.Errorf("Body = %q, want %q", s.Body, "# Hello World\n\nSome body content.\n")
	}
}

func TestParseFile_MissingFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := writeSpec(t, dir, "no-fm.md", "# Just a markdown file\n\nNo frontmatter here.\n")

	_, err := ParseFile(path)
	if err == nil {
		t.Fatal("expected error for missing frontmatter")
	}
}

func TestParseFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := writeSpec(t, dir, "empty.md", "")

	_, err := ParseFile(path)
	if err == nil {
		t.Fatal("expected error for empty file")
	}
}

func TestParseFile_NoEndDelimiter(t *testing.T) {
	dir := t.TempDir()
	path := writeSpec(t, dir, "no-end.md", "---\ntitle: Test\nstatus: drafted\n")

	_, err := ParseFile(path)
	if err == nil {
		t.Fatal("expected error for missing closing delimiter")
	}
}

func TestParseBytes_NilAffects(t *testing.T) {
	content := "---\ntitle: Test\nstatus: drafted\ntrack: local\neffort: small\ncreated: 2026-01-01\nupdated: 2026-01-01\nauthor: test\ndispatched_task_id: null\n---\n"

	s, err := ParseBytes([]byte(content), "test.md")
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if s.Affects == nil {
		t.Error("Affects is nil, want empty slice")
	}
	if s.DependsOn == nil {
		t.Error("DependsOn is nil, want empty slice")
	}
}
