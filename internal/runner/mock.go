package runner

import (
	"context"
	"sync"
	"time"

	"changkun.de/wallfacer/internal/store"
	"changkun.de/wallfacer/internal/workspace"
	"changkun.de/wallfacer/prompts"
	"github.com/google/uuid"
)

// MockRunner is a lightweight test double that implements Interface. It records
// calls to the most useful methods so tests can assert that handlers triggered
// the expected side-effects (e.g. RunBackground, KillContainer). All other
// methods are no-ops that return safe zero values.
//
// MockRunner is intentionally placed in a non-_test.go file so that it can be
// imported from integration tests in package main_test (or any other package).
// It is not intended for production use.
type MockRunner struct {
	mu sync.Mutex

	// Configuration fields — set these before calling NewHandler.
	EnvFilePath string
	Cmd         string
	Image       string
	WtDir       string
	CodexPath   string

	// Recorded call arguments (mutex-protected for race-safety).
	RunBackgroundCalls        []uuid.UUID
	KillContainerCalls        []uuid.UUID
	KillRefineContainerCalls  []uuid.UUID
	CleanupWorktreesCalls     []uuid.UUID
	GenerateTitleCalls        []uuid.UUID

	// Optional overrides for ContainerName / RefineContainerName return values.
	// When nil the methods return "" (no container active), matching the default
	// behaviour expected by most tests.
	ContainerNameFn       func(taskID uuid.UUID) string
	RefineContainerNameFn func(taskID uuid.UUID) string
}

// compile-time assertion.
var _ Interface = (*MockRunner)(nil)

// RunCalls returns a race-safe snapshot of the RunBackground call IDs.
func (m *MockRunner) RunCalls() []uuid.UUID {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]uuid.UUID, len(m.RunBackgroundCalls))
	copy(out, m.RunBackgroundCalls)
	return out
}

// KillCalls returns a race-safe snapshot of the KillContainer call IDs.
func (m *MockRunner) KillCalls() []uuid.UUID {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]uuid.UUID, len(m.KillContainerCalls))
	copy(out, m.KillContainerCalls)
	return out
}

// KillRefineCalls returns a race-safe snapshot of the KillRefineContainer call IDs.
func (m *MockRunner) KillRefineCalls() []uuid.UUID {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]uuid.UUID, len(m.KillRefineContainerCalls))
	copy(out, m.KillRefineContainerCalls)
	return out
}

func (m *MockRunner) RunBackground(taskID uuid.UUID, _, _ string, _ bool) {
	m.mu.Lock()
	m.RunBackgroundCalls = append(m.RunBackgroundCalls, taskID)
	m.mu.Unlock()
}

func (m *MockRunner) Commit(_ uuid.UUID, _ string) error { return nil }

func (m *MockRunner) SyncWorktreesBackground(_ uuid.UUID, _ string, _ store.TaskStatus, _ ...func()) {
}

func (m *MockRunner) RunRefinementBackground(_ uuid.UUID, _ string) {}

func (m *MockRunner) Fork(_ context.Context, _, _ uuid.UUID) error { return nil }

func (m *MockRunner) EnsureTaskWorktrees(_ uuid.UUID, existing map[string]string, branchName string) (map[string]string, string, error) {
	return existing, branchName, nil
}

func (m *MockRunner) CleanupWorktrees(taskID uuid.UUID, _ map[string]string, _ string) {
	m.mu.Lock()
	m.CleanupWorktreesCalls = append(m.CleanupWorktreesCalls, taskID)
	m.mu.Unlock()
}

func (m *MockRunner) PruneUnknownWorktrees() {}

func (m *MockRunner) ListContainers() ([]ContainerInfo, error) { return nil, nil }

func (m *MockRunner) ContainerName(taskID uuid.UUID) string {
	if m.ContainerNameFn != nil {
		return m.ContainerNameFn(taskID)
	}
	return ""
}

func (m *MockRunner) RefineContainerName(taskID uuid.UUID) string {
	if m.RefineContainerNameFn != nil {
		return m.RefineContainerNameFn(taskID)
	}
	return ""
}

func (m *MockRunner) KillContainer(taskID uuid.UUID) {
	m.mu.Lock()
	m.KillContainerCalls = append(m.KillContainerCalls, taskID)
	m.mu.Unlock()
}

func (m *MockRunner) KillRefineContainer(taskID uuid.UUID) {
	m.mu.Lock()
	m.KillRefineContainerCalls = append(m.KillRefineContainerCalls, taskID)
	m.mu.Unlock()
}

func (m *MockRunner) ContainerCircuitAllow() bool { return true }

func (m *MockRunner) ContainerCircuitState() string { return "closed" }

func (m *MockRunner) ContainerCircuitFailures() int { return 0 }

func (m *MockRunner) RecordContainerFailure() {}

func (m *MockRunner) PendingGoroutines() []string { return nil }

func (m *MockRunner) WaitBackground() {}

func (m *MockRunner) GenerateTitleBackground(taskID uuid.UUID, _ string) {
	m.mu.Lock()
	m.GenerateTitleCalls = append(m.GenerateTitleCalls, taskID)
	m.mu.Unlock()
}

func (m *MockRunner) GenerateOversight(_ uuid.UUID) {}

func (m *MockRunner) GenerateBoardManifest(_ context.Context, _ uuid.UUID, _ bool) (*BoardManifest, error) {
	return &BoardManifest{}, nil
}

func (m *MockRunner) BuildIdeationPrompt(_ []store.Task) string { return "" }

func (m *MockRunner) CreateIdeaBacklogTasks(_ context.Context, _ uuid.UUID) error { return nil }

func (m *MockRunner) IdeationCategories() []string { return nil }

func (m *MockRunner) IdeationIgnorePatterns() []string { return nil }

func (m *MockRunner) HostCodexAuthStatus(_ time.Time) (bool, string) { return false, "" }

func (m *MockRunner) CodexAuthPath() string { return m.CodexPath }

func (m *MockRunner) ShutdownCtx() context.Context { return context.Background() }

func (m *MockRunner) Command() string { return m.Cmd }

func (m *MockRunner) SandboxImage() string { return m.Image }

func (m *MockRunner) WorktreesDir() string { return m.WtDir }

func (m *MockRunner) EnvFile() string { return m.EnvFilePath }

func (m *MockRunner) Prompts() *prompts.Manager { return nil }

func (m *MockRunner) WorkspaceManager() *workspace.Manager { return nil }
