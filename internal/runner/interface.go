package runner

import (
	"context"
	"time"

	"changkun.de/wallfacer/internal/store"
	"changkun.de/wallfacer/internal/workspace"
	"changkun.de/wallfacer/prompts"
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
	Fork(ctx context.Context, sourceID, newTaskID uuid.UUID) error

	// Worktree management.
	EnsureTaskWorktrees(taskID uuid.UUID, existing map[string]string, branchName string) (map[string]string, string, error)
	CleanupWorktrees(taskID uuid.UUID, worktreePaths map[string]string, branchName string)
	PruneUnknownWorktrees()

	// Container management.
	ListContainers() ([]ContainerInfo, error)
	ContainerName(taskID uuid.UUID) string
	RefineContainerName(taskID uuid.UUID) string
	KillContainer(taskID uuid.UUID)
	KillRefineContainer(taskID uuid.UUID)

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

	// Ideation.
	BuildIdeationPrompt(existingTasks []store.Task) string
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
	WorktreesDir() string
	EnvFile() string
	Prompts() *prompts.Manager
	WorkspaceManager() *workspace.Manager
}

// compile-time assertion: *Runner satisfies Interface.
var _ Interface = (*Runner)(nil)
