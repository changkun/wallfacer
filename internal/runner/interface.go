package runner

import (
	"context"
	"time"

	"github.com/google/uuid"
	"latere.ai/x/wallfacer/internal/agents"
	"latere.ai/x/wallfacer/internal/executor"
	"latere.ai/x/wallfacer/internal/flow"
	"latere.ai/x/wallfacer/internal/harness"
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
	ContainerName(taskID uuid.UUID) string
	TaskLogReader(taskID uuid.UUID) *livelog.Reader
	KillContainer(taskID uuid.UUID)
	StopTaskWorker(taskID uuid.UUID)
	WorkerStats() executor.WorkerStatsInfo

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
	// not have a task ID in scope, e.g. the plan commit pipeline.
	GenerateCommitMessage(ctx context.Context, data prompts.CommitData) (string, error)

	// Drift assessment (task-free flavor). Runs a one-shot agent comparing a
	// spec against a task's actual changes; the drift pipeline consumes the
	// verdict. Gated behind WALLFACER_DRIFT_TESTER at the call site.
	AssessDrift(ctx context.Context, specBody string, affects, changedFiles []string, diff string) (spec.DriftVerdict, error)

	// RunCriticRound runs a one-shot stateless agent invocation for an agon
	// critic turn in the given working directory (cwd), so the critic can read
	// the full codebase rather than only the diff patch. cwd may be empty for a
	// patch-only critic. Returns raw markdown text. Token usage is not
	// attributed to any task (agon aggregates it separately).
	RunCriticRound(ctx context.Context, prompt string, sb harness.ID, cwd string, deadline time.Duration) (string, error)

	// Agent-session title generation (task-free flavor). Names a chat
	// thread from its opening user message using the lightweight title model.
	GenerateAgentSessionTitle(ctx context.Context, firstUserMessage string) (string, error)

	// Auto-push for a single workspace (task-free flavor). Used by callers
	// that do not have a task ID in scope, e.g. the plan commit pipeline.
	MaybeAutoPushWorkspace(ctx context.Context, ws string)

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
	SandboxBackend() executor.Backend
	WorktreesDir() string
	TmpDir() string
	EnvFile() string
	Prompts() *prompts.Manager
	WorkspaceManager() *workspace.Manager

	// Agents catalog accessors (merged built-in + user-authored).
	AgentsRegistry() *agents.Registry
	AgentsDir() string
	ReloadAgents() error

	// Flows catalog accessors (merged built-in + user-authored).
	FlowsRegistry() *flow.Registry
	FlowsDir() string
	ReloadFlows() error
}

// compile-time assertion: *Runner satisfies Interface.
var _ Interface = (*Runner)(nil)
