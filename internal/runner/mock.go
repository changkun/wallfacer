package runner

import (
	"context"
	"slices"
	"sync"
	"time"

	"changkun.de/x/wallfacer/internal/agents"
	"changkun.de/x/wallfacer/internal/flow"
	"changkun.de/x/wallfacer/internal/prompts"
	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
	"changkun.de/x/wallfacer/internal/workspace"
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
	Host        bool // HostMode() return value

	// Recorded call arguments (mutex-protected for race-safety).
	RunBackgroundCalls          []uuid.UUID
	KillContainerCalls          []uuid.UUID
	CleanupWorktreesCalls       []uuid.UUID
	GenerateTitleCalls          []uuid.UUID
	MaybeAutoPushWorkspaceCalls []string

	// Optional override for ContainerName return value.
	// When nil the method returns "" (no container active), matching the default
	// behaviour expected by most tests.
	ContainerNameFn func(taskID uuid.UUID) string

	// GenerateCommitMessageFn lets tests stub the task-free commit-message
	// generator. When nil the method returns an empty string and a nil
	// error so callers fall back to their deterministic path.
	GenerateCommitMessageFn func(ctx context.Context, data prompts.CommitData) (string, error)
}

// compile-time assertion.
var _ Interface = (*MockRunner)(nil)

// RunCalls returns a race-safe snapshot of the RunBackground call IDs.
func (m *MockRunner) RunCalls() []uuid.UUID {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.RunBackgroundCalls)
}

// KillCalls returns a race-safe snapshot of the KillContainer call IDs.
func (m *MockRunner) KillCalls() []uuid.UUID {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.KillContainerCalls)
}

// RunBackground records the call and returns immediately.
func (m *MockRunner) RunBackground(taskID uuid.UUID, _, _ string, _ bool) {
	m.mu.Lock()
	m.RunBackgroundCalls = append(m.RunBackgroundCalls, taskID)
	m.mu.Unlock()
}

// Commit is a no-op mock.
func (m *MockRunner) Commit(_ uuid.UUID, _ string) error { return nil }

// SyncWorktreesBackground is a no-op mock.
func (m *MockRunner) SyncWorktreesBackground(_ uuid.UUID, _ string, _ store.TaskStatus, _ ...func()) {
}

// EnsureTaskWorktrees returns the provided worktrees unchanged.
func (m *MockRunner) EnsureTaskWorktrees(_ uuid.UUID, existing map[string]string, branchName string) (map[string]string, string, error) {
	return existing, branchName, nil
}

// CleanupWorktrees records the call for later assertions.
func (m *MockRunner) CleanupWorktrees(taskID uuid.UUID, _ map[string]string, _ string) {
	m.mu.Lock()
	m.CleanupWorktreesCalls = append(m.CleanupWorktreesCalls, taskID)
	m.mu.Unlock()
}

// PruneUnknownWorktrees is a no-op mock.
func (m *MockRunner) PruneUnknownWorktrees() {}

// ListContainers returns an empty list.
func (m *MockRunner) ListContainers() ([]sandbox.ContainerInfo, error) { return nil, nil }

// ContainerName returns the container name for a task, using ContainerNameFn if set.
func (m *MockRunner) ContainerName(taskID uuid.UUID) string {
	if m.ContainerNameFn != nil {
		return m.ContainerNameFn(taskID)
	}
	return ""
}

// TaskLogReader returns nil in the mock (no live logs).
func (m *MockRunner) TaskLogReader(_ uuid.UUID) *LiveLogReader { return nil }

// KillContainer records a kill-container call.
func (m *MockRunner) KillContainer(taskID uuid.UUID) {
	m.mu.Lock()
	m.KillContainerCalls = append(m.KillContainerCalls, taskID)
	m.mu.Unlock()
}

// StopTaskWorker is a no-op in the mock.
func (m *MockRunner) StopTaskWorker(_ uuid.UUID) {}

// WorkerStats returns empty stats in the mock.
func (m *MockRunner) WorkerStats() sandbox.WorkerStatsInfo { return sandbox.WorkerStatsInfo{} }

// ContainerCircuitAllow always returns true in the mock.
func (m *MockRunner) ContainerCircuitAllow() bool { return true }

// ContainerCircuitState returns the circuit breaker state.
func (m *MockRunner) ContainerCircuitState() string { return "closed" }

// ContainerCircuitFailures returns the failure count.
func (m *MockRunner) ContainerCircuitFailures() int { return 0 }

// RecordContainerFailure is a no-op in the mock.
func (m *MockRunner) RecordContainerFailure() {}

// PendingGoroutines returns nil in the mock.
func (m *MockRunner) PendingGoroutines() []string { return nil }

// WaitBackground is a no-op in the mock.
func (m *MockRunner) WaitBackground() {}

// GenerateTitleBackground records a title-generation call.
func (m *MockRunner) GenerateTitleBackground(taskID uuid.UUID, _ string) {
	m.mu.Lock()
	m.GenerateTitleCalls = append(m.GenerateTitleCalls, taskID)
	m.mu.Unlock()
}

// GenerateOversight is a no-op in the mock.
func (m *MockRunner) GenerateOversight(_ uuid.UUID) {}

// GenerateBoardManifest returns an empty manifest in the mock.
func (m *MockRunner) GenerateBoardManifest(_ context.Context, _ uuid.UUID, _ bool) (*BoardManifest, error) {
	return &BoardManifest{}, nil
}

// BuildIdeationPrompt returns an empty prompt in the mock.
func (m *MockRunner) BuildIdeationPrompt(_ []store.Task) string { return "" }

// CreateIdeaBacklogTasks is a no-op in the mock.
func (m *MockRunner) CreateIdeaBacklogTasks(_ context.Context, _ uuid.UUID) error { return nil }

// IdeationCategories returns nil in the mock.
func (m *MockRunner) IdeationCategories() []string { return nil }

// IdeationIgnorePatterns returns nil in the mock.
func (m *MockRunner) IdeationIgnorePatterns() []string { return nil }

// HostCodexAuthStatus returns false in the mock.
func (m *MockRunner) HostCodexAuthStatus(_ time.Time) (bool, string) { return false, "" }

// CodexAuthPath returns the configured codex auth path.
func (m *MockRunner) CodexAuthPath() string { return m.CodexPath }

// ShutdownCtx returns a background context in the mock.
func (m *MockRunner) ShutdownCtx() context.Context { return context.Background() }

// Command returns the configured command.
func (m *MockRunner) Command() string { return m.Cmd }

// SandboxImage returns the configured sandbox image.
func (m *MockRunner) SandboxImage() string { return m.Image }

// SandboxBackend returns nil (mock does not provide a real backend).
func (m *MockRunner) SandboxBackend() sandbox.Backend { return nil }

// HostMode reports the mock's configured hostMode (defaults to false).
func (m *MockRunner) HostMode() bool { return m.Host }

// WorktreesDir returns the configured worktrees directory.
func (m *MockRunner) WorktreesDir() string { return m.WtDir }

// TmpDir returns the base directory for ephemeral container-mounted files.
func (m *MockRunner) TmpDir() string { return "" }

// EnvFile returns the configured env file path.
func (m *MockRunner) EnvFile() string { return m.EnvFilePath }

// Prompts returns a default prompts manager (empty overrides dir) so tests
// that construct a Handler with a MockRunner don't panic calling
// h.runner.Prompts().PromptsDir(). Override via m.PromptsFn if a test needs
// a specific manager.
func (m *MockRunner) Prompts() *prompts.Manager { return prompts.NewManager("") }

// GenerateCommitMessage delegates to GenerateCommitMessageFn when set; the
// default returns ("", nil) so callers hit their deterministic fallback
// path without signalling an error.
func (m *MockRunner) GenerateCommitMessage(ctx context.Context, data prompts.CommitData) (string, error) {
	if m.GenerateCommitMessageFn != nil {
		return m.GenerateCommitMessageFn(ctx, data)
	}
	return "", nil
}

// MaybeAutoPushWorkspace records the workspace path for later inspection by tests.
func (m *MockRunner) MaybeAutoPushWorkspace(_ context.Context, ws string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.MaybeAutoPushWorkspaceCalls = append(m.MaybeAutoPushWorkspaceCalls, ws)
}

// AutoPushWorkspaceCalls returns a race-safe snapshot of the workspaces passed to MaybeAutoPushWorkspace.
func (m *MockRunner) AutoPushWorkspaceCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.MaybeAutoPushWorkspaceCalls)
}

// WorkspaceManager returns nil.
func (m *MockRunner) WorkspaceManager() *workspace.Manager { return nil }

// AgentsRegistry returns the built-in catalog. Sufficient for handler
// tests that need slug lookups without hitting the disk.
func (m *MockRunner) AgentsRegistry() *agents.Registry { return agents.NewBuiltinRegistry() }

// AgentsDir returns an empty string. Handler tests that exercise the
// write path set this via a concrete Runner instead of the mock.
func (m *MockRunner) AgentsDir() string { return "" }

// ReloadAgents is a no-op on the mock.
func (m *MockRunner) ReloadAgents() error { return nil }

// FlowsRegistry returns the built-in flow catalog. Mirrors
// AgentsRegistry for symmetry with the concrete Runner.
func (m *MockRunner) FlowsRegistry() *flow.Registry { return flow.NewBuiltinRegistry() }

// FlowsDir returns an empty string on the mock.
func (m *MockRunner) FlowsDir() string { return "" }

// ReloadFlows is a no-op on the mock.
func (m *MockRunner) ReloadFlows() error { return nil }
