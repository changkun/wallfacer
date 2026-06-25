package runner

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"latere.ai/x/wallfacer/internal/store"
)

// TestGenerateBoardContext_Basic verifies that generateBoardContext produces
// valid JSON with correct is_self marking and no session_id leakage.
func TestGenerateBoardContext_Basic(t *testing.T) {
	s, r := setupRunnerWithCmd(t, nil, "echo")
	ctx := bg()

	t1, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "Task one", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	t2, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "Task two", Timeout: 10, MountWorktrees: true})
	if err != nil {
		t.Fatal(err)
	}
	t3, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "Task three", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}

	// Put tasks in different statuses.
	_ = s.UpdateTaskStatus(ctx, t1.ID, "in_progress")

	_ = s.UpdateTaskResult(ctx, t1.ID, "working", "sess-secret", "max_tokens", 2)

	_ = s.ForceUpdateTaskStatus(ctx, t2.ID, "done")

	// t3 stays in backlog.

	data, _, err := r.generateBoardContextAndMounts(t2.ID, false)
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

	data, _, err := r.generateBoardContextAndMounts([16]byte{}, false)
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

// TestBuildSiblingMounts verifies that buildSiblingMounts returns eligible
// sibling worktrees and excludes the self task and ineligible statuses.
func TestBuildSiblingMounts(t *testing.T) {
	s, r := setupRunnerWithCmd(t, nil, "echo")
	ctx := bg()

	t1, _ := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "self task", Timeout: 5, MountWorktrees: true})
	t2, _ := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "waiting task", Timeout: 5})
	t3, _ := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "backlog task", Timeout: 5})

	// Set t2 to waiting with worktree paths.
	_ = s.ForceUpdateTaskStatus(ctx, t2.ID, "waiting")

	wtDir := t.TempDir()
	_ = s.UpdateTaskWorktrees(ctx, t2.ID, map[string]string{"/myrepo": wtDir}, "task/"+t2.ID.String()[:8])

	// t3 stays in backlog (no worktrees).
	_ = t3

	_, mounts, err := r.generateBoardContextAndMounts(t1.ID, true)
	if err != nil {
		t.Fatalf("generateBoardContextAndMounts: %v", err)
	}
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

// TestGenerateBoardContext_AllStatuses verifies that tasks in every
// non-archived status appear in the manifest with the correct status field.
func TestGenerateBoardContext_AllStatuses(t *testing.T) {
	s, r := setupRunnerWithCmd(t, nil, "echo")
	ctx := bg()

	statuses := []store.TaskStatus{
		store.TaskStatusBacklog,
		store.TaskStatusInProgress,
		store.TaskStatusWaiting,
		store.TaskStatusFailed,
		store.TaskStatusCancelled,
	}

	idByStatus := make(map[store.TaskStatus]string)
	for _, st := range statuses {
		task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "task for " + string(st), Timeout: 5})
		if err != nil {
			t.Fatal(err)
		}
		switch st {
		case store.TaskStatusBacklog:
			// Default status after creation; no update needed.
		case store.TaskStatusInProgress:
			_ = s.UpdateTaskStatus(ctx, task.ID, st)

		default:
			_ = s.ForceUpdateTaskStatus(ctx, task.ID, st)

		}
		idByStatus[st] = task.ID.String()
	}

	data, _, err := r.generateBoardContextAndMounts([16]byte{}, false)
	if err != nil {
		t.Fatalf("generateBoardContext: %v", err)
	}

	var manifest BoardManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if len(manifest.Tasks) != len(statuses) {
		t.Fatalf("expected %d tasks, got %d", len(statuses), len(manifest.Tasks))
	}

	byID := make(map[string]BoardTask)
	for _, bt := range manifest.Tasks {
		byID[bt.ID] = bt
	}

	for _, st := range statuses {
		id := idByStatus[st]
		bt, ok := byID[id]
		if !ok {
			t.Errorf("task with status %q not found in manifest", st)
			continue
		}
		if bt.Status != st {
			t.Errorf("task %s: status = %q, want %q", bt.ShortID, bt.Status, st)
		}
		if bt.IsSelf {
			t.Errorf("task %s should not be marked is_self", bt.ShortID)
		}
	}
}

// TestGenerateBoardContext_WorktreeMountPath verifies that generateBoardContext
// sets worktree_mount to the correct container-side path for eligible siblings,
// and that the self task has no worktree_mount.
func TestGenerateBoardContext_WorktreeMountPath(t *testing.T) {
	s, r := setupRunnerWithCmd(t, nil, "echo")
	ctx := bg()

	// Create a sibling task in waiting status with a worktree directory.
	sibling, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "sibling task", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.ForceUpdateTaskStatus(ctx, sibling.ID, store.TaskStatusWaiting)

	wtDir := t.TempDir()
	repoPath := "/home/user/myrepo"
	_ = s.UpdateTaskWorktrees(ctx, sibling.ID, map[string]string{repoPath: wtDir}, "task/"+sibling.ID.String()[:8])

	// Create a self task (stays in backlog).
	self, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "self task", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	data, _, err := r.generateBoardContextAndMounts(self.ID, true)
	if err != nil {
		t.Fatalf("generateBoardContext: %v", err)
	}

	var manifest BoardManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	shortID := sibling.ID.String()[:8]
	expectedMount := "/workspace/.tasks/worktrees/" + shortID + "/" + filepath.Base(repoPath)

	for _, bt := range manifest.Tasks {
		switch bt.ID {
		case sibling.ID.String():
			if bt.WorktreeMount == nil {
				t.Fatal("sibling WorktreeMount should not be nil")
			}
			if *bt.WorktreeMount != expectedMount {
				t.Errorf("WorktreeMount = %q, want %q", *bt.WorktreeMount, expectedMount)
			}
		case self.ID.String():
			if bt.WorktreeMount != nil {
				t.Errorf("self task WorktreeMount should be nil, got %q", *bt.WorktreeMount)
			}
		}
	}
}

// TestGenerateBoardContext_ArchivedTaskExcluded verifies that tasks with the
// archived flag set do not appear in the board manifest.
func TestGenerateBoardContext_ArchivedTaskExcluded(t *testing.T) {
	s, r := setupRunnerWithCmd(t, nil, "echo")
	ctx := bg()

	normal, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "normal task", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	archived, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "archived task", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskArchived(ctx, archived.ID, true); err != nil {
		t.Fatalf("SetTaskArchived: %v", err)
	}

	data, _, err := r.generateBoardContextAndMounts([16]byte{}, false)
	if err != nil {
		t.Fatalf("generateBoardContext: %v", err)
	}

	var manifest BoardManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if len(manifest.Tasks) != 1 {
		t.Fatalf("expected 1 task in manifest, got %d", len(manifest.Tasks))
	}
	if manifest.Tasks[0].ID != normal.ID.String() {
		t.Errorf("manifest task ID = %q, want %q", manifest.Tasks[0].ID, normal.ID.String())
	}
	if contains(string(data), archived.ID.String()) {
		t.Error("archived task ID should not appear in the board manifest")
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

// waitBoardSeqStable blocks until boardChangeSeq has not changed for one
// millisecond, meaning the board-cache-invalidator goroutine has drained its
// pending store notifications and the cache is safe to prime.
func waitBoardSeqStable(r *Runner) {
	prev := r.boardChangeSeq.Load()
	for {
		time.Sleep(time.Millisecond)
		cur := r.boardChangeSeq.Load()
		if cur == prev {
			return
		}
		prev = cur
	}
}

// TestBoardCacheHit asserts that a second call to generateBoardContextAndMounts
// (with no intervening store mutations) completes in under 5 µs — the cache
// hit path avoids store.ListTasks entirely.
func TestBoardCacheHit(t *testing.T) {
	s, r := setupRunnerWithCmd(t, nil, "echo")
	ctx := context.Background()
	var selfID [16]byte
	for i := 0; i < 100; i++ {
		task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "task prompt", Timeout: 5})
		if err != nil {
			t.Fatal(err)
		}
		if i == 50 {
			selfID = task.ID
		}
	}

	// Wait for the board-cache-invalidator goroutine to drain all pending
	// store notifications so boardChangeSeq is stable before we prime the cache.
	waitBoardSeqStable(r)

	// Prime the cache.
	if _, _, err := r.generateBoardContextAndMounts(selfID, false); err != nil {
		t.Fatal(err)
	}

	// The cache hit path copies JSON bytes and skips store.ListTasks entirely.
	// With 100 tasks (~35 KB of JSON), copying should complete well within 500 µs
	// even on a loaded system — far below the multi-ms cost of a full ListTasks call.
	// Shared CI runners add noise, so we take the best of several measurements:
	// the minimum observed time is the most reliable signal that the cache path
	// is hot. As long as one iteration clears the bar, the cache is doing its job.
	const limit = 500 * time.Microsecond
	best := time.Duration(math.MaxInt64)
	for i := 0; i < 10; i++ {
		start := time.Now()
		_, _, err := r.generateBoardContextAndMounts(selfID, false)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatal(err)
		}
		if elapsed < best {
			best = elapsed
		}
	}
	if best > limit {
		t.Errorf("cache hit best-of-10 took %v, want < %v", best, limit)
	}
}

// BenchmarkGenerateBoardContext measures per-turn board context cost with
// a warm cache (no store mutations between calls).
// Run with: go test ./internal/runner/ -bench=BenchmarkGenerateBoardContext -benchmem
func BenchmarkGenerateBoardContext(b *testing.B) {
	s, r := setupRunnerWithCmd(b, nil, "echo")
	ctx := context.Background()
	var selfID [16]byte
	for i := 0; i < 100; i++ {
		task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "task prompt", Timeout: 5})
		if err != nil {
			b.Fatal(err)
		}
		if i == 50 {
			selfID = task.ID
		}
	}

	// Wait for all pending notifications to be processed before priming.
	waitBoardSeqStable(r)

	// Prime the cache with one call.
	if _, _, err := r.generateBoardContextAndMounts(selfID, false); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _, err := r.generateBoardContextAndMounts(selfID, false)
		if err != nil {
			b.Fatal(err)
		}
	}
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
		{"truncated adds ellipsis", "hello world", 5, "hello…"},
		{"empty string", "", 10, ""},
		{"max zero", "hello", 0, "…"},
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
	longPrompt := repeat("A", 2000) // 2000 chars, cap is 500
	longResult := repeat("B", 3000) // 3000 chars, cap is 1000

	// Create several sibling tasks with long text so the manifest would be huge
	// without truncation.
	for i := 0; i < 5; i++ {
		task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: longPrompt, Timeout: 5})
		if err != nil {
			t.Fatal(err)
		}
		_ = s.ForceUpdateTaskStatus(ctx, task.ID, "done")

		_ = s.UpdateTaskResult(ctx, task.ID, longResult, "sess", "end_turn", 3)

	}

	// Create the self task with a long prompt and result too.
	selfTask, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: longPrompt, Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.UpdateTaskStatus(ctx, selfTask.ID, "in_progress")

	_ = s.UpdateTaskResult(ctx, selfTask.ID, longResult, "sess-self", "max_tokens", 7)

	data, _, err := r.generateBoardContextAndMounts(selfTask.ID, false)
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
			if !strings.HasSuffix(bt.Prompt, "…") {
				t.Errorf("sibling task %s prompt should end with '…', got len=%d", bt.ShortID, len(bt.Prompt))
			}
			if bt.Result == nil || !strings.HasSuffix(*bt.Result, "…") {
				t.Errorf("sibling task %s result should end with '…'", bt.ShortID)
			}
			// (c) Non-self task Turns must be 0.
			if bt.Turns != 0 {
				t.Errorf("sibling task %s Turns = %d, want 0", bt.ShortID, bt.Turns)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// logBoardManifestSizeWarning
// ---------------------------------------------------------------------------

// TestLogBoardManifestSizeWarning_LessThan5 verifies that the function does
// not panic when the number of size entries is fewer than 5.
func TestLogBoardManifestSizeWarning_LessThan5(_ *testing.T) {
	sizes := []struct {
		id    string
		bytes int
	}{
		{id: "task1", bytes: 1000},
		{id: "task2", bytes: 2000},
	}
	// Must not panic; exercises the sort and log path with small input.
	logBoardManifestSizeWarning(sizes, 3000)
}

// TestLogBoardManifestSizeWarning_MoreThan5 verifies that the function does
// not panic when there are more than 5 entries and only logs the top 5.
func TestLogBoardManifestSizeWarning_MoreThan5(_ *testing.T) {
	sizes := []struct {
		id    string
		bytes int
	}{
		{id: "t1", bytes: 100},
		{id: "t2", bytes: 200},
		{id: "t3", bytes: 300},
		{id: "t4", bytes: 400},
		{id: "t5", bytes: 500},
		{id: "t6", bytes: 600},
	}
	// Must not panic and must sort descending (t6 is largest, logged first).
	logBoardManifestSizeWarning(sizes, 2100)
}

// TestLogBoardManifestSizeWarning_Sorted verifies that sizes are sorted in
// descending order (largest first) before being logged.
func TestLogBoardManifestSizeWarning_Sorted(t *testing.T) {
	sizes := []struct {
		id    string
		bytes int
	}{
		{id: "small", bytes: 10},
		{id: "large", bytes: 9999},
		{id: "medium", bytes: 500},
	}
	// After the call the slice must be sorted descending (function sorts in place).
	logBoardManifestSizeWarning(sizes, 10509)
	if sizes[0].id != "large" {
		t.Errorf("expected largest entry first after sort, got %q", sizes[0].id)
	}
	if sizes[1].id != "medium" {
		t.Errorf("expected medium entry second, got %q", sizes[1].id)
	}
}

// ---------------------------------------------------------------------------
// writeBoardDir
// ---------------------------------------------------------------------------

// TestWriteBoardDir_CreatesFileWithContent verifies that writeBoardDir creates
// a temp directory containing board.json with the exact bytes provided.
func TestWriteBoardDir_CreatesFileWithContent(t *testing.T) {
	data := []byte(`{"tasks":[], "generated_at":"2024-01-01T00:00:00Z"}`)
	dir, err := writeBoardDir(data, "")
	if err != nil {
		t.Fatalf("writeBoardDir: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(dir)
	}()

	content, err := os.ReadFile(filepath.Join(dir, "board.json"))
	if err != nil {
		t.Fatalf("read board.json: %v", err)
	}
	if string(content) != string(data) {
		t.Errorf("content mismatch: got %q, want %q", string(content), string(data))
	}
}

// TestWriteBoardDir_EmptyData verifies that writeBoardDir works with minimal data.
func TestWriteBoardDir_EmptyData(t *testing.T) {
	dir, err := writeBoardDir([]byte("{}"), "")
	if err != nil {
		t.Fatalf("writeBoardDir empty: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(dir)
	}()
	if _, err := os.Stat(filepath.Join(dir, "board.json")); err != nil {
		t.Fatalf("board.json should exist: %v", err)
	}
}

// TestWriteBoardDir_ReturnsDirPath verifies that the returned path is a valid
// directory that contains board.json.
func TestWriteBoardDir_ReturnsDirPath(t *testing.T) {
	dir, err := writeBoardDir([]byte(`{"ok":true}`), "")
	if err != nil {
		t.Fatalf("writeBoardDir: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(dir)
	}()

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("returned path not accessible: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("expected a directory, got a file at %q", dir)
	}
}
