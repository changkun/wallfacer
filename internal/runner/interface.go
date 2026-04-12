package runner

import (
	"context"
	"time"

	"changkun.de/x/wallfacer/internal/prompts"
	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
	"changkun.de/x/wallfacer/internal/workspace"
	"github.com/google/uuid"
)

// Interface is the set of methods on *Runner that internal/handler/ calls. It
// allows tests to substitute a lightweight mock without spinning up a real
// container runtime.
type Interface interface {
	// Task execution.
	RunBackground(taskID uuid.UUID, prompt, sessionID string, resumedFromWaiting bool)
	Commit(taskID uuid.UUID, sessionID string) error
	SyncWorktreesBackground(taskID uuid.UUID, sessionID string, prevStatus store.TaskStatus, onDone ...func())
	RunRefinementBackground(taskID uuid.UUID, userInstructions string)

	// Worktree management.
	EnsureTaskWorktrees(taskID uuid.UUID, existing map[string]string, branchName string) (map[string]string, string, error)
	CleanupWorktrees(taskID uuid.UUID, worktreePaths map[string]string, branchName string)
	PruneUnknownWorktrees()

	// Container management.
	ListContainers() ([]sandbox.ContainerInfo, error)
	ContainerName(taskID uuid.UUID) string
	TaskLogReader(taskID uuid.UUID) *LiveLogReader
	RefineContainerName(taskID uuid.UUID) string
	KillContainer(taskID uuid.UUID)
	KillRefineContainer(taskID uuid.UUID)
	StopTaskWorker(taskID uuid.UUID)
	WorkerStats() sandbox.WorkerStatsInfo

	// Container circuit breaker.
	ContainerCircuitAllow() bool
	ContainerCircuitState() string
	ContainerCircuitFailures() int
	RecordContainerFailure()

	// Background goroutine tracking.
	PendingGoroutines() []string
	WaitBackground()

	// Title & oversight generation.
	GenerateTitleBackground(taskID uuid.UUID, prompt string)
	GenerateOversight(taskID uuid.UUID)
	GenerateBoardManifest(ctx context.Context, selfTaskID uuid.UUID, mountWorktrees bool) (*BoardManifest, error)

	// Commit-message generation (task-free flavor). Used by callers that do
	// not have a task ID in scope, e.g. the planning commit pipeline.
	GenerateCommitMessage(ctx context.Context, data prompts.CommitData) (string, error)

	// Ideation.
	BuildIdeationPrompt(existingTasks []store.Task) string
	CreateIdeaBacklogTasks(ctx context.Context, taskID uuid.UUID) error
	IdeationCategories() []string
	IdeationIgnorePatterns() []string

	// Host Codex auth.
	HostCodexAuthStatus(now time.Time) (bool, string)
	CodexAuthPath() string

	// Shutdown context.
	ShutdownCtx() context.Context

	// Configuration accessors.
	Command() string
	SandboxImage() string
	SandboxBackend() sandbox.Backend
	WorktreesDir() string
	TmpDir() string
	EnvFile() string
	Prompts() *prompts.Manager
	WorkspaceManager() *workspace.Manager
}

// compile-time assertion: *Runner satisfies Interface.
var _ Interface = (*Runner)(nil)
