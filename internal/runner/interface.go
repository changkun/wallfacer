package runner

import (
	"context"
	"time"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/executor"
	"latere.ai/x/wallfacer/internal/pkg/livelog"
	"latere.ai/x/wallfacer/internal/prompts"
	"latere.ai/x/wallfacer/internal/spec"
	"latere.ai/x/wallfacer/internal/store"
	"latere.ai/x/wallfacer/internal/workspace"
)

// Interface is the set of methods on *Runner that internal/handler/ calls. It
// allows tests to substitute a lightweight mock without spinning up a real
// container runtime.
type Interface interface {
	// Task execution.
	RunBackground(taskID uuid.UUID, prompt, sessionID string, resumedFromWaiting bool)
	Commit(taskID uuid.UUID, sessionID string) error
	SyncWorktreesBackground(taskID uuid.UUID, sessionID string, prevStatus store.TaskStatus, onDone ...func())

	// Worktree management.
	EnsureTaskWorktrees(taskID uuid.UUID, existing map[string]string, branchName string) (map[string]string, string, error)
	CleanupWorktrees(taskID uuid.UUID, worktreePaths map[string]string, branchName string)
	PruneUnknownWorktrees()

	// Container management.
	ListContainers() ([]executor.ContainerInfo, error)
	TaskLogReader(taskID uuid.UUID) *livelog.Reader
	KillContainer(taskID uuid.UUID)

	// Background goroutine tracking.
	PendingGoroutines() []string
	WaitBackground()

	// Title & oversight generation.
	GenerateTitleBackground(taskID uuid.UUID, prompt string)
	GenerateOversight(taskID uuid.UUID)
	GenerateBoardManifest(ctx context.Context, selfTaskID uuid.UUID, mountWorktrees bool) (*BoardManifest, error)

	// Commit-message generation (task-free flavor). Used by callers that do
	// not have a task ID in scope, e.g. the plan commit pipeline.
	GenerateCommitMessage(ctx context.Context, data prompts.CommitData) (string, error)

	// Drift assessment (task-free flavor). Runs a one-shot agent comparing a
	// spec against a task's actual changes; the drift pipeline consumes the
	// verdict. Gated behind WALLFACER_DRIFT_TESTER at the call site.
	AssessDrift(ctx context.Context, specBody string, affects, changedFiles []string, diff string) (spec.DriftVerdict, error)

	// Agent-session title generation (task-free flavor). Names a chat
	// thread from its opening user message using the lightweight title model.
	GenerateAgentSessionTitle(ctx context.Context, firstUserMessage string) (string, error)

	// Host Codex auth.
	HostCodexAuthStatus(now time.Time) (bool, string)
	CodexAuthPath() string

	// Shutdown context.
	ShutdownCtx() context.Context

	// Configuration accessors.
	SandboxBackend() executor.Backend
	WorktreesDir() string
	TmpDir() string
	EnvFile() string
	WorkspaceManager() *workspace.Manager
}

// compile-time assertion: *Runner satisfies Interface.
var _ Interface = (*Runner)(nil)
