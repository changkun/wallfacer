package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"changkun.de/wallfacer/internal/store"
)

// TestGenerateBoardContext_Basic verifies that generateBoardContext produces
// valid JSON with correct is_self marking and no session_id leakage.
func TestGenerateBoardContext_Basic(t *testing.T) {
	s, r := setupRunnerWithCmd(t, nil, "echo")
	ctx := bg()

	t1, err := s.CreateTask(ctx, "Task one", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	t2, err := s.CreateTask(ctx, "Task two", 10, true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	t3, err := s.CreateTask(ctx, "Task three", 15, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Put tasks in different statuses.
	s.UpdateTaskStatus(ctx, t1.ID, "in_progress")
	s.UpdateTaskResult(ctx, t1.ID, "working", "sess-secret", "max_tokens", 2)
	s.ForceUpdateTaskStatus(ctx, t2.ID, "done")
	// t3 stays in backlog.

	data, err := r.generateBoardContext(t2.ID, false)
	if err != nil {
		t.Fatalf("generateBoardContext: %v", err)
	}

	var manifest BoardManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if manifest.SelfTaskID != t2.ID.String() {
		t.Errorf("SelfTaskID = %q, want %q", manifest.SelfTaskID, t2.ID.String())
	}
	if len(manifest.Tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(manifest.Tasks))
	}

	// Verify is_self marking.
	for _, bt := range manifest.Tasks {
		if bt.ID == t2.ID.String() {
			if !bt.IsSelf {
				t.Error("t2 should be marked is_self=true")
			}
		} else {
			if bt.IsSelf {
				t.Errorf("task %s should not be is_self", bt.ID)
			}
		}
	}

	// Verify no session_id in the raw JSON output.
	if json.Valid(data) {
		raw := string(data)
		if contains(raw, "sess-secret") {
			t.Error("session_id should not appear in board.json output")
		}
	}

	// Verify ShortID is 8 characters.
	for _, bt := range manifest.Tasks {
		if len(bt.ShortID) != 8 {
			t.Errorf("ShortID %q should be 8 chars", bt.ShortID)
		}
	}

	_ = t1
	_ = t3
}

// TestGenerateBoardContext_Empty verifies that an empty task list produces
// an empty array (not null) in the JSON.
func TestGenerateBoardContext_Empty(t *testing.T) {
	_, r := setupRunnerWithCmd(t, nil, "echo")

	data, err := r.generateBoardContext([16]byte{}, false)
	if err != nil {
		t.Fatalf("generateBoardContext: %v", err)
	}

	var manifest BoardManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if manifest.Tasks == nil {
		t.Error("Tasks should be an empty slice, not nil")
	}
	if len(manifest.Tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(manifest.Tasks))
	}
}

// TestCanMountWorktree is a table-driven test for all task statuses.
func TestCanMountWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	existingWT := map[string]string{"/repo": tmpDir}
	noWT := map[string]string(nil)

	cases := []struct {
		status store.TaskStatus
		wt     map[string]string
		want   bool
	}{
		{store.TaskStatusBacklog, existingWT, false},
		{store.TaskStatusInProgress, existingWT, false},
		{store.TaskStatusWaiting, existingWT, true},
		{store.TaskStatusFailed, existingWT, true},
		{store.TaskStatusDone, existingWT, true},
		{store.TaskStatusDone, noWT, false},
		{store.TaskStatusDone, map[string]string{"/repo": "/nonexistent/path"}, false},
		{store.TaskStatusCancelled, existingWT, false},
		{"archived", existingWT, false},
	}

	for _, tc := range cases {
		got := canMountWorktree(tc.status, tc.wt)
		if got != tc.want {
			t.Errorf("canMountWorktree(%q, %v) = %v, want %v", tc.status, tc.wt, got, tc.want)
		}
	}
}

// TestPrepareBoardContext verifies that prepareBoardContext creates a temp
// directory with a valid board.json file.
func TestPrepareBoardContext(t *testing.T) {
	s, r := setupRunnerWithCmd(t, nil, "echo")
	ctx := bg()

	task, err := s.CreateTask(ctx, "test task", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	dir, err := r.prepareBoardContext(task.ID, false)
	if err != nil {
		t.Fatalf("prepareBoardContext: %v", err)
	}
	defer os.RemoveAll(dir)

	boardPath := filepath.Join(dir, "board.json")
	data, err := os.ReadFile(boardPath)
	if err != nil {
		t.Fatalf("board.json should exist: %v", err)
	}

	var manifest BoardManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("invalid board.json: %v", err)
	}
	if len(manifest.Tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(manifest.Tasks))
	}
}

// TestBuildSiblingMounts verifies that buildSiblingMounts returns eligible
// sibling worktrees and excludes the self task and ineligible statuses.
func TestBuildSiblingMounts(t *testing.T) {
	s, r := setupRunnerWithCmd(t, nil, "echo")
	ctx := bg()

	t1, _ := s.CreateTask(ctx, "self task", 5, true, "", "")
	t2, _ := s.CreateTask(ctx, "waiting task", 5, false, "", "")
	t3, _ := s.CreateTask(ctx, "backlog task", 5, false, "", "")

	// Set t2 to waiting with worktree paths.
	s.ForceUpdateTaskStatus(ctx, t2.ID, "waiting")
	wtDir := t.TempDir()
	s.UpdateTaskWorktrees(ctx, t2.ID, map[string]string{"/myrepo": wtDir}, "task/"+t2.ID.String()[:8])

	// t3 stays in backlog (no worktrees).
	_ = t3

	mounts := r.buildSiblingMounts(t1.ID)
	if mounts == nil {
		t.Fatal("expected non-nil sibling mounts")
	}

	shortID := t2.ID.String()[:8]
	repos, ok := mounts[shortID]
	if !ok {
		t.Fatalf("expected mount for shortID %s", shortID)
	}
	if repos["/myrepo"] != wtDir {
		t.Errorf("expected worktree path %q, got %q", wtDir, repos["/myrepo"])
	}

	// Self task should not appear.
	selfShort := t1.ID.String()[:8]
	if _, ok := mounts[selfShort]; ok {
		t.Error("self task should not appear in sibling mounts")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsString(s, substr))
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// bg returns a background context (convenience alias used by store tests).
func bg() context.Context {
	return context.Background()
}

// ---------------------------------------------------------------------------
// truncate helper
// ---------------------------------------------------------------------------

func TestTruncate(t *testing.T) {
	cases := []struct {
		name  string
		input string
		max   int
		want  string
	}{
		{"short string unchanged", "hello", 10, "hello"},
		{"exact length unchanged", "hello", 5, "hello"},
		{"truncated adds ellipsis", "hello world", 5, "hello..."},
		{"empty string", "", 10, ""},
		{"max zero", "hello", 0, "..."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncate(tc.input, tc.max)
			if got != tc.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.max, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Field truncation and size limiting in generateBoardContext
// ---------------------------------------------------------------------------

// repeat returns s repeated n times (helper for constructing long strings).
func repeat(s string, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(s)
	}
	return b.String()
}

// TestGenerateBoardContext_TruncationAndSizeLimit verifies that:
// (a) the output JSON stays within the 64 KB limit when tasks have long text,
// (b) truncation markers "..." are present for sibling task text that was cut,
// (c) non-self task Turns are 0,
// (d) the self task retains its full prompt and result without truncation.
func TestGenerateBoardContext_TruncationAndSizeLimit(t *testing.T) {
	s, r := setupRunnerWithCmd(t, nil, "echo")
	ctx := bg()

	// Build prompts and results that far exceed the per-field caps.
	longPrompt := repeat("A", 2000)  // 2000 chars, cap is 500
	longResult := repeat("B", 3000)  // 3000 chars, cap is 1000

	// Create several sibling tasks with long text so the manifest would be huge
	// without truncation.
	for i := 0; i < 5; i++ {
		task, err := s.CreateTask(ctx, longPrompt, 5, false, "", "")
		if err != nil {
			t.Fatal(err)
		}
		s.ForceUpdateTaskStatus(ctx, task.ID, "done")
		s.UpdateTaskResult(ctx, task.ID, longResult, "sess", "end_turn", 3)
	}

	// Create the self task with a long prompt and result too.
	selfTask, err := s.CreateTask(ctx, longPrompt, 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	s.UpdateTaskStatus(ctx, selfTask.ID, "in_progress")
	s.UpdateTaskResult(ctx, selfTask.ID, longResult, "sess-self", "max_tokens", 7)

	data, err := r.generateBoardContext(selfTask.ID, false)
	if err != nil {
		t.Fatalf("generateBoardContext: %v", err)
	}

	// (a) JSON must be within 64 KB.
	const maxBytes = 64 * 1024
	if len(data) > maxBytes {
		t.Errorf("board manifest size %d exceeds 64 KB limit", len(data))
	}

	var manifest BoardManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	for _, bt := range manifest.Tasks {
		if bt.IsSelf {
			// (d) Self task must NOT be truncated.
			if bt.Prompt != longPrompt {
				t.Errorf("self task prompt was truncated (len=%d, want %d)", len(bt.Prompt), len(longPrompt))
			}
			if bt.Result == nil || *bt.Result != longResult {
				resultLen := 0
				if bt.Result != nil {
					resultLen = len(*bt.Result)
				}
				t.Errorf("self task result was truncated (len=%d, want %d)", resultLen, len(longResult))
			}
			// Self task Turns should carry the real value.
			if bt.Turns == 0 {
				t.Error("self task Turns should be non-zero")
			}
		} else {
			// (b) Truncation marker must be present when original was longer than cap.
			if !strings.HasSuffix(bt.Prompt, "...") {
				t.Errorf("sibling task %s prompt should end with '...', got len=%d", bt.ShortID, len(bt.Prompt))
			}
			if bt.Result == nil || !strings.HasSuffix(*bt.Result, "...") {
				t.Errorf("sibling task %s result should end with '...'", bt.ShortID)
			}
			// (c) Non-self task Turns must be 0.
			if bt.Turns != 0 {
				t.Errorf("sibling task %s Turns = %d, want 0", bt.ShortID, bt.Turns)
			}
		}
	}
}
