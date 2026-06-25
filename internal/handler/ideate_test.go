package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store"
)

// --- /api/ideate shims ---
//
// The Automation toggle that used to own ideation is gone: ideation is
// now a Kind=idea-agent task the user creates from the standard
// composer (optionally recurring via the routine primitive). The
// /api/ideate endpoints remain as thin shims so CLI and automation
// scripts do not break.

func TestTriggerIdeation_ReturnsAccepted(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/ideate", nil)
	w := httptest.NewRecorder()
	h.TriggerIdeation(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if queued, _ := resp["queued"].(bool); !queued {
		t.Errorf("expected queued=true, got %v", resp)
	}
	taskID, _ := resp["task_id"].(string)
	if taskID == "" {
		t.Errorf("expected non-empty task_id, got %v", resp)
	}
}

func TestTriggerIdeation_RejectsParallelFlights(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "existing ideation", Timeout: 15, Kind: store.TaskKindIdeaAgent,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/ideate", nil)
	w := httptest.NewRecorder()
	h.TriggerIdeation(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 when an idea-agent task is in flight", w.Code)
	}
}

func TestCancelIdeation_NoTasks(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/ideate", nil)
	w := httptest.NewRecorder()
	h.CancelIdeation(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["cancelled"] {
		t.Error("expected cancelled=false when no tasks exist")
	}
}

func TestCancelIdeation_CancelsBacklogIdeaAgentTask(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "brainstorm prompt", Timeout: 15, Kind: store.TaskKindIdeaAgent,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.Status != store.TaskStatusBacklog {
		t.Fatalf("expected backlog status, got %s", task.Status)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/ideate", nil)
	w := httptest.NewRecorder()
	h.CancelIdeation(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp["cancelled"] {
		t.Error("expected cancelled=true for backlogged idea-agent task")
	}

	// The HTTP response claiming cancelled=true is not enough: assert the
	// task is actually cancelled in the store. backlog -> cancelled is not a
	// valid state-machine transition, so a plain UpdateTaskStatus would fail
	// silently and leave the task in backlog while the handler still reported
	// cancelled=true. CancelIdeation must force the cancellation.
	got, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != store.TaskStatusCancelled {
		t.Errorf("task status = %s in store, want cancelled", got.Status)
	}
}

func TestCancelIdeation_IgnoresNonIdeaAgentTasks(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	_, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "regular task", Timeout: 15,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/ideate", nil)
	w := httptest.NewRecorder()
	h.CancelIdeation(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["cancelled"] {
		t.Error("expected cancelled=false: non-idea-agent tasks must not be cancelled")
	}
}

func TestGetIdeationStatus_ReturnsEnabledFalseAndRunningFlag(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/ideate", nil)
	w := httptest.NewRecorder()
	h.GetIdeationStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if enabled, _ := resp["enabled"].(bool); enabled {
		t.Errorf("expected enabled=false after toggle retirement, got %v", resp)
	}
	if _, ok := resp["running"]; !ok {
		t.Error("response missing 'running' field")
	}
}

func TestGetIdeationStatus_RunningReflectsLiveIdeaAgent(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "brainstorm", Timeout: 15, Kind: store.TaskKindIdeaAgent,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/ideate", nil)
	w := httptest.NewRecorder()
	h.GetIdeationStatus(w, req)

	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if running, _ := resp["running"].(bool); !running {
		t.Error("expected running=true when an idea-agent task is in_progress")
	}
}

// --- Legacy routine cleanup ---

func TestCleanupLegacyIdeationRoutine_RemovesGhostCards(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	// Simulate a store left behind by a prior deployment: a routine card
	// tagged system:ideation that cleanupLegacyIdeationRoutine should
	// remove on the first reconcile.
	routine, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt:                 "old ideation routine",
		Kind:                   store.TaskKindRoutine,
		Tags:                   []string{systemIdeationTag},
		Timeout:                15,
		RoutineIntervalSeconds: 3600,
		RoutineEnabled:         true,
		RoutineSpawnKind:       store.TaskKindIdeaAgent,
	})
	if err != nil {
		t.Fatalf("seed routine: %v", err)
	}

	h.cleanupLegacyIdeationRoutine(ctx, h.store)

	tasks, _ := h.store.ListTasks(ctx, true)
	for _, tk := range tasks {
		if tk.ID == routine.ID {
			t.Fatalf("expected legacy routine deleted, still present with status %s", tk.Status)
		}
	}
}

func TestCleanupLegacyIdeationRoutine_LeavesRegularRoutines(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	user, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt:                 "my weekly routine",
		Kind:                   store.TaskKindRoutine,
		Tags:                   []string{"ops"},
		Timeout:                15,
		RoutineIntervalSeconds: 3600,
		RoutineEnabled:         true,
	})
	if err != nil {
		t.Fatalf("seed user routine: %v", err)
	}

	h.cleanupLegacyIdeationRoutine(ctx, h.store)

	got, err := h.store.GetTask(ctx, user.ID)
	if err != nil {
		t.Fatalf("user routine should still exist: %v", err)
	}
	if !slices.Contains(got.Tags, "ops") {
		t.Fatalf("user routine tags unexpectedly changed: %v", got.Tags)
	}
}

// --- RunBackground integration smoke ---

// TestTriggerIdeation_UsesMockRunner ensures the trigger path still
// calls RunBackground with the spawned task's ID. Catches a regression
// where the shim stops actually firing the agent.
func TestTriggerIdeation_UsesMockRunner(t *testing.T) {
	mock := &runner.MockRunner{}
	h, _ := newTestHandlerWithMockRunner(t, mock)
	req := httptest.NewRequest(http.MethodPost, "/api/ideate", nil)
	w := httptest.NewRecorder()
	h.TriggerIdeation(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d: %s", w.Code, strings.TrimSpace(w.Body.String()))
	}
	calls := mock.RunCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 RunBackground call, got %d", len(calls))
	}
}

// newTestHandlerWithStoreDirAndMock builds a handler backed by a mock runner
// (so KillContainer is a controlled no-op) and returns the store's root
// directory so tests can manipulate on-disk permissions to inject failures.
func newTestHandlerWithStoreDirAndMock(t *testing.T, mock *runner.MockRunner) (*Handler, string) {
	t.Helper()
	storeDir, err := os.MkdirTemp("", "wallfacer-ideate-fail-*")
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.NewFileStore(storeDir)
	if err != nil {
		_ = os.RemoveAll(storeDir)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Restore perms so cleanup can remove the tree.
		_ = filepath.Walk(storeDir, func(p string, _ os.FileInfo, _ error) error {
			_ = os.Chmod(p, 0o755)
			return nil
		})
		_ = os.RemoveAll(storeDir)
	})
	t.Cleanup(s.WaitCompaction)
	return &Handler{runner: mock, store: s}, storeDir
}

// TestCancelIdeation_StatusWriteFailureDoesNotReportCancelled verifies that when
// the store status write fails on a valid transition, CancelIdeation does not
// report cancelled=true and leaves the task in its prior status. Before the fix,
// the error was discarded and cancelled=true was returned for a transition that
// never persisted. Exercises the in_progress -> cancelled path (a valid
// transition that reaches the disk write); the read-only data dir makes the
// atomic task.json write fail.
func TestCancelIdeation_StatusWriteFailureDoesNotReportCancelled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows: chmod 0500 on a dir does not block file writes inside it")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: read-only dir does not block writes")
	}
	mock := &runner.MockRunner{}
	h, storeDir := newTestHandlerWithStoreDirAndMock(t, mock)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "brainstorm", Timeout: 15, Kind: store.TaskKindIdeaAgent,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Move to in_progress so that in_progress -> cancelled is a valid transition
	// that reaches saveTask.
	if err := h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus in_progress: %v", err)
	}

	// Make the task's data directory read-only so the atomic task.json write fails.
	taskDir := filepath.Join(storeDir, task.ID.String())
	if err := os.Chmod(taskDir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/ideate", nil)
	w := httptest.NewRecorder()
	h.CancelIdeation(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["cancelled"] {
		t.Error("expected cancelled=false when the status write failed")
	}

	// Restore perms so the test cleanup can remove the tree.
	_ = os.Chmod(taskDir, 0o755)
}
