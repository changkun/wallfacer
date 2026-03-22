package runner

import (
	"context"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// TestMockRunner_RunBackground_RecordsCalls verifies that RunBackground records
// the task ID in the call log.
func TestMockRunner_RunBackground_RecordsCalls(t *testing.T) {
	m := &MockRunner{}
	id := uuid.New()
	m.RunBackground(id, "msg", "sess", false)
	calls := m.RunCalls()
	if len(calls) != 1 || calls[0] != id {
		t.Errorf("expected RunBackground to record call, got %v", calls)
	}
}

// TestMockRunner_KillContainer_RecordsCalls verifies that KillContainer records
// the task ID in the call log.
func TestMockRunner_KillContainer_RecordsCalls(t *testing.T) {
	m := &MockRunner{}
	id := uuid.New()
	m.KillContainer(id)
	calls := m.KillCalls()
	if len(calls) != 1 || calls[0] != id {
		t.Errorf("expected KillContainer to record call, got %v", calls)
	}
}

// TestMockRunner_CleanupWorktrees_RecordsCalls verifies that CleanupWorktrees
// records the task ID in the call log.
func TestMockRunner_CleanupWorktrees_RecordsCalls(t *testing.T) {
	m := &MockRunner{}
	id := uuid.New()
	m.CleanupWorktrees(id, nil, "")

	m.mu.Lock()
	recorded := m.CleanupWorktreesCalls
	m.mu.Unlock()

	if len(recorded) != 1 || recorded[0] != id {
		t.Errorf("expected CleanupWorktrees to record call, got %v", recorded)
	}
}

// TestMockRunner_GenerateTitle_RecordsCalls verifies that GenerateTitleBackground
// records the task ID in the call log.
func TestMockRunner_GenerateTitle_RecordsCalls(t *testing.T) {
	m := &MockRunner{}
	id := uuid.New()
	m.GenerateTitleBackground(id, "prompt")

	m.mu.Lock()
	recorded := m.GenerateTitleCalls
	m.mu.Unlock()

	if len(recorded) != 1 || recorded[0] != id {
		t.Errorf("expected 1 title call, got %d: %v", len(recorded), recorded)
	}
}

// TestMockRunner_MultipleCalls verifies that multiple calls are accumulated.
func TestMockRunner_MultipleCalls(t *testing.T) {
	m := &MockRunner{}
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}

	for _, id := range ids {
		m.RunBackground(id, "msg", "sess", false)
		m.KillContainer(id)
	}

	runCalls := m.RunCalls()
	if len(runCalls) != len(ids) {
		t.Errorf("expected %d RunBackground calls, got %d", len(ids), len(runCalls))
	}
	killCalls := m.KillCalls()
	if len(killCalls) != len(ids) {
		t.Errorf("expected %d KillContainer calls, got %d", len(ids), len(killCalls))
	}
}

// TestMockRunner_ContainerNameFn verifies that ContainerNameFn and
// RefineContainerNameFn overrides are respected.
func TestMockRunner_ContainerNameFn(t *testing.T) {
	id := uuid.New()
	m := &MockRunner{
		ContainerNameFn: func(taskID uuid.UUID) string {
			return "mock-container-" + taskID.String()
		},
		RefineContainerNameFn: func(taskID uuid.UUID) string {
			return "mock-refine-" + taskID.String()
		},
	}

	got := m.ContainerName(id)
	want := "mock-container-" + id.String()
	if got != want {
		t.Errorf("ContainerName = %q, want %q", got, want)
	}

	gotRefine := m.RefineContainerName(id)
	wantRefine := "mock-refine-" + id.String()
	if gotRefine != wantRefine {
		t.Errorf("RefineContainerName = %q, want %q", gotRefine, wantRefine)
	}
}

// TestMockRunner_ContainerName_Default verifies nil fn returns empty string.
func TestMockRunner_ContainerName_Default(t *testing.T) {
	m := &MockRunner{}
	id := uuid.New()
	if got := m.ContainerName(id); got != "" {
		t.Errorf("ContainerName with nil fn = %q, want %q", got, "")
	}
	if got := m.RefineContainerName(id); got != "" {
		t.Errorf("RefineContainerName with nil fn = %q, want %q", got, "")
	}
}

// TestMockRunner_NoOpMethods verifies all no-op methods don't panic and return
// safe zero values.
func TestMockRunner_NoOpMethods(t *testing.T) {
	m := &MockRunner{
		Cmd:         "echo",
		Image:       "test:latest",
		WtDir:       "/tmp/wt",
		EnvFilePath: "/tmp/env",
		CodexPath:   "/tmp/codex",
	}
	id := uuid.New()
	ctx := context.Background()

	if err := m.Commit(id, "sess"); err != nil {
		t.Errorf("Commit returned unexpected error: %v", err)
	}

	m.SyncWorktreesBackground(id, "sess", store.TaskStatusWaiting)
	m.RunRefinementBackground(id, "sess")

	paths, branch, err := m.EnsureTaskWorktrees(id, nil, "mybranch")
	if err != nil {
		t.Errorf("EnsureTaskWorktrees returned unexpected error: %v", err)
	}
	if branch != "mybranch" {
		t.Errorf("EnsureTaskWorktrees branch = %q, want %q", branch, "mybranch")
	}
	_ = paths

	m.PruneUnknownWorktrees()

	containers, cerr := m.ListContainers()
	if cerr != nil {
		t.Errorf("ListContainers returned unexpected error: %v", cerr)
	}
	if containers != nil {
		t.Errorf("ListContainers returned non-nil: %v", containers)
	}

	m.KillRefineContainer(id)

	if got := m.ContainerCircuitAllow(); !got {
		t.Errorf("ContainerCircuitAllow = false, want true")
	}
	if got := m.ContainerCircuitState(); got != "closed" {
		t.Errorf("ContainerCircuitState = %q, want %q", got, "closed")
	}
	if got := m.ContainerCircuitFailures(); got != 0 {
		t.Errorf("ContainerCircuitFailures = %d, want 0", got)
	}

	m.RecordContainerFailure()

	if got := m.PendingGoroutines(); got != nil {
		t.Errorf("PendingGoroutines = %v, want nil", got)
	}

	m.WaitBackground()
	m.GenerateOversight(id)

	manifest, mErr := m.GenerateBoardManifest(ctx, id, false)
	if mErr != nil {
		t.Errorf("GenerateBoardManifest returned unexpected error: %v", mErr)
	}
	if manifest == nil {
		t.Error("GenerateBoardManifest returned nil manifest")
	}

	if got := m.BuildIdeationPrompt(nil); got != "" {
		t.Errorf("BuildIdeationPrompt = %q, want empty", got)
	}

	if got := m.IdeationCategories(); got != nil {
		t.Errorf("IdeationCategories = %v, want nil", got)
	}
	if got := m.IdeationIgnorePatterns(); got != nil {
		t.Errorf("IdeationIgnorePatterns = %v, want nil", got)
	}

	ok, msg := m.HostCodexAuthStatus(time.Now())
	if ok {
		t.Errorf("HostCodexAuthStatus ok = true, want false")
	}
	_ = msg

	if got := m.CodexAuthPath(); got != m.CodexPath {
		t.Errorf("CodexAuthPath = %q, want %q", got, m.CodexPath)
	}

	if got := m.ShutdownCtx(); got == nil {
		t.Error("ShutdownCtx returned nil context")
	}

	if got := m.Command(); got != m.Cmd {
		t.Errorf("Command = %q, want %q", got, m.Cmd)
	}
	if got := m.SandboxImage(); got != m.Image {
		t.Errorf("SandboxImage = %q, want %q", got, m.Image)
	}
	if got := m.WorktreesDir(); got != m.WtDir {
		t.Errorf("WorktreesDir = %q, want %q", got, m.WtDir)
	}
	if got := m.EnvFile(); got != m.EnvFilePath {
		t.Errorf("EnvFile = %q, want %q", got, m.EnvFilePath)
	}

	if got := m.Prompts(); got != nil {
		t.Errorf("Prompts = %v, want nil", got)
	}
	if got := m.WorkspaceManager(); got != nil {
		t.Errorf("WorkspaceManager = %v, want nil", got)
	}
}

// TestMockRunner_EnsureTaskWorktrees_PropagatesExisting verifies that
// EnsureTaskWorktrees returns the provided existing map and branch as-is.
func TestMockRunner_EnsureTaskWorktrees_PropagatesExisting(t *testing.T) {
	m := &MockRunner{}
	id := uuid.New()
	existing := map[string]string{"/ws/repo": "/wt/branch"}
	paths, branch, err := m.EnsureTaskWorktrees(id, existing, "feat/my-branch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branch != "feat/my-branch" {
		t.Errorf("branch = %q, want %q", branch, "feat/my-branch")
	}
	if len(paths) != len(existing) {
		t.Errorf("paths len = %d, want %d", len(paths), len(existing))
	}
	for k, v := range existing {
		if paths[k] != v {
			t.Errorf("paths[%q] = %q, want %q", k, paths[k], v)
		}
	}
}
