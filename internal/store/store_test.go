// Tests for store.go: NewStore, loadAll, loadEvents, OutputsDir, Close,
// and full persistence round-trip integration tests.
package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestNewStore_EmptyDir(t *testing.T) {
	s := newTestStore(t)
	tasks, err := s.ListTasks(bg(), false)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestNewStore_CreatesDirectoryRecursively(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore with nested path: %v", err)
	}
	s.Close()
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("directory not created: %v", err)
	}
}

func TestNewStore_SkipsNonUUIDDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "not-a-uuid"), 0755); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	tasks, _ := s.ListTasks(bg(), false)
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestNewStore_SkipsUUIDDirWithMissingTaskJSON(t *testing.T) {
	dir := t.TempDir()
	id := uuid.New()
	if err := os.MkdirAll(filepath.Join(dir, id.String()), 0755); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	tasks, _ := s.ListTasks(bg(), false)
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestNewStore_SkipsCorruptTaskJSON(t *testing.T) {
	dir := t.TempDir()
	id := uuid.New()
	taskDir := filepath.Join(dir, id.String())
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "task.json"), []byte("{invalid json}"), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	tasks, _ := s.ListTasks(bg(), false)
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestNewStore_LoadsExistingTask(t *testing.T) {
	dir := t.TempDir()
	s1, _ := NewStore(dir)
	task, _ := s1.CreateTask(bg(), "hello", 10)

	s2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("reload NewStore: %v", err)
	}
	got, err := s2.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask after reload: %v", err)
	}
	if got.Prompt != "hello" {
		t.Errorf("Prompt = %q, want 'hello'", got.Prompt)
	}
}

func TestClose_IsNoOp(t *testing.T) {
	s := newTestStore(t)
	s.Close() // must not panic
}

func TestOutputsDir(t *testing.T) {
	s := newTestStore(t)
	id := uuid.New()
	want := filepath.Join(s.dir, id.String(), "outputs")
	if got := s.OutputsDir(id); got != want {
		t.Errorf("OutputsDir = %q, want %q", got, want)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Full persistence round-trip integration tests
// ─────────────────────────────────────────────────────────────────────────────

func TestPersistence_FullRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)

	task, _ := s.CreateTask(bg(), "round trip prompt", 15)
	s.UpdateTaskStatus(bg(), task.ID, "in_progress")
	s.UpdateTaskTitle(bg(), task.ID, "Round Trip Title")
	s.AccumulateTaskUsage(bg(), task.ID, TaskUsage{InputTokens: 100, CostUSD: 0.5})
	s.UpdateTaskWorktrees(bg(), task.ID, map[string]string{"/repo": "/wt"}, "task/rt")
	s.InsertEvent(bg(), task.ID, EventTypeStateChange, "in_progress")
	s.InsertEvent(bg(), task.ID, EventTypeOutput, "some output")

	s2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("reload NewStore: %v", err)
	}

	got, err := s2.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask after reload: %v", err)
	}
	if got.Prompt != "round trip prompt" {
		t.Errorf("Prompt = %q", got.Prompt)
	}
	if got.Status != "in_progress" {
		t.Errorf("Status = %q", got.Status)
	}
	if got.Title != "Round Trip Title" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.Timeout != 15 {
		t.Errorf("Timeout = %d", got.Timeout)
	}
	if got.Usage.InputTokens != 100 {
		t.Errorf("InputTokens = %d", got.Usage.InputTokens)
	}
	if got.BranchName != "task/rt" {
		t.Errorf("BranchName = %q", got.BranchName)
	}

	events, _ := s2.GetEvents(bg(), task.ID)
	if len(events) != 2 {
		t.Errorf("expected 2 events after reload, got %d", len(events))
	}
}

func TestPersistence_DeletedTaskGoneAfterReload(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	task, _ := s.CreateTask(bg(), "delete me", 5)
	s.DeleteTask(bg(), task.ID)

	s2, _ := NewStore(dir)
	if _, err := s2.GetTask(bg(), task.ID); err == nil {
		t.Error("expected task to be absent after delete + reload")
	}
}
