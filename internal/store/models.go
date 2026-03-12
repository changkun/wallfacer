package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
)

// TaskUsage tracks token consumption and cost for a task across all turns.
// Each container invocation in -p mode reports per-invocation totals (not
// session-cumulative), so values are accumulated directly without deltas.
type TaskUsage struct {
	InputTokens          int     `json:"input_tokens"`
	OutputTokens         int     `json:"output_tokens"`
	CacheReadInputTokens int     `json:"cache_read_input_tokens"`
	CacheCreationTokens  int     `json:"cache_creation_input_tokens"`
	CostUSD              float64 `json:"cost_usd"`
}

// ExecutionEnvironment captures the runtime environment used for a task execution.
// It is recorded once at the start of Run() and stored alongside the task for
// reproducibility auditing and debugging when results differ between runs.
type ExecutionEnvironment struct {
	ContainerImage   string    `json:"container_image"`   // e.g. "wallfacer-sandbox"
	ContainerDigest  string    `json:"container_digest"`  // sha256 of image, empty if unavailable
	ModelName        string    `json:"model_name"`        // e.g. "claude-opus-4-6"
	APIBaseURL       string    `json:"api_base_url"`      // empty string = default Anthropic endpoint
	InstructionsHash string    `json:"instructions_hash"` // sha256 hex of CLAUDE.md at run start
	Sandbox          string    `json:"sandbox"`           // configured sandbox: "claude", "codex", etc.
	RecordedAt       time.Time `json:"recorded_at"`
}

// TurnUsageRecord captures token consumption and stop reason for a single agent turn.
type TurnUsageRecord struct {
	Turn                 int       `json:"turn"`
	Timestamp            time.Time `json:"timestamp"`
	InputTokens          int       `json:"input_tokens"`
	OutputTokens         int       `json:"output_tokens"`
	CacheReadInputTokens int       `json:"cache_read_input_tokens"`
	CacheCreationTokens  int       `json:"cache_creation_tokens"`
	CostUSD              float64   `json:"cost_usd"`
	StopReason           string    `json:"stop_reason,omitempty"`
	Sandbox              string    `json:"sandbox,omitempty"`
	SubAgent             string    `json:"sub_agent,omitempty"` // "implementation", "test", "refinement", etc.
}

// RefinementMessage is a single turn in a refinement chat session.
// Kept for backward compatibility with older chat-based refinement sessions.
type RefinementMessage struct {
	Role      string    `json:"role"` // "user" or "assistant"
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// RefinementSession records a single sandbox-based refinement run.
// StartPrompt is the task prompt at the beginning of the session.
// Result is the raw spec produced by the sandbox agent.
// ResultPrompt is the prompt the user applied (may differ from Result if edited).
// Messages is kept for backward compatibility with older chat-based sessions.
type RefinementSession struct {
	ID           string              `json:"id"`
	CreatedAt    time.Time           `json:"created_at"`
	StartPrompt  string              `json:"start_prompt"`
	Messages     []RefinementMessage `json:"messages,omitempty"`
	Result       string              `json:"result,omitempty"`
	ResultPrompt string              `json:"result_prompt,omitempty"`
}

// RetryRecord captures the execution outcome of one task lifecycle before it
// is reset for a retry. Appended to Task.RetryHistory by ResetTaskForRetry.
type RetryRecord struct {
	RetiredAt time.Time  `json:"retired_at"`
	Prompt    string     `json:"prompt"`
	Status    TaskStatus `json:"status"`
	Result    string     `json:"result,omitempty"` // truncated to 2000 chars
	SessionID string     `json:"session_id,omitempty"`
	Turns     int        `json:"turns"`
	CostUSD   float64    `json:"cost_usd"`
}

// RefinementJob tracks the state of an active or recently completed
// sandbox refinement run for a backlog task.
type RefinementJob struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Status    string    `json:"status"` // "running", "done", "failed"
	Result    string    `json:"result,omitempty"`
	Error     string    `json:"error,omitempty"`
	// source indicates who created the job. "runner" jobs originate from the
	// UI-triggered refinement flow and may be briefly treated as in-flight while
	// async startup/failure races settle.
	Source string `json:"source,omitempty"`
}

// TaskKind identifies the execution mode for a task.
// The zero value ("") and "task" both mean a standard implementation task.
// "idea-agent" is a special task that runs the brainstorm agent: it analyses
// the workspaces, proposes ideas, and creates backlog tasks from the results.
type TaskKind = string

const (
	TaskKindTask      TaskKind = ""           // default; regular implementation task
	TaskKindIdeaAgent TaskKind = "idea-agent" // brainstorm / ideation task
)

const (
	SandboxActivityImplementation = "implementation"
	SandboxActivityTesting        = "testing"
	SandboxActivityRefinement     = "refinement"
	SandboxActivityTitle          = "title"
	SandboxActivityOversight      = "oversight"
	SandboxActivityCommitMessage  = "commit_message"
	SandboxActivityIdeaAgent      = "idea_agent"
)

var SandboxActivities = []string{
	SandboxActivityImplementation,
	SandboxActivityTesting,
	SandboxActivityRefinement,
	SandboxActivityTitle,
	SandboxActivityOversight,
	SandboxActivityCommitMessage,
	SandboxActivityIdeaAgent,
}

// TaskStatus represents the lifecycle state of a task.
type TaskStatus string

const (
	TaskStatusBacklog    TaskStatus = "backlog"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusWaiting    TaskStatus = "waiting"
	TaskStatusCommitting TaskStatus = "committing"
	TaskStatusDone       TaskStatus = "done"
	TaskStatusFailed     TaskStatus = "failed"
	TaskStatusCancelled  TaskStatus = "cancelled"
)

// ErrInvalidTransition is returned by UpdateTaskStatus when the requested
// status change is not permitted by the task state machine.
var ErrInvalidTransition = errors.New("invalid transition")

// allowedTransitions encodes the complete task state machine. Only transitions
// present in this map are accepted by UpdateTaskStatus; all others are rejected.
var allowedTransitions = map[TaskStatus][]TaskStatus{
	TaskStatusBacklog:    {TaskStatusInProgress},
	TaskStatusInProgress: {TaskStatusWaiting, TaskStatusFailed, TaskStatusCancelled},
	TaskStatusCommitting: {TaskStatusDone, TaskStatusFailed},
	TaskStatusWaiting:    {TaskStatusInProgress, TaskStatusCommitting, TaskStatusCancelled},
	TaskStatusFailed:     {TaskStatusBacklog, TaskStatusCancelled},
	TaskStatusDone:       {TaskStatusCancelled},
	TaskStatusCancelled:  {TaskStatusBacklog},
}

// ValidateTransition returns nil if transitioning from `from` to `to` is
// permitted by the task state machine, or a descriptive error wrapping
// ErrInvalidTransition if it is not.
func ValidateTransition(from, to TaskStatus) error {
	for _, allowed := range allowedTransitions[from] {
		if allowed == to {
			return nil
		}
	}
	return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, from, to)
}

// CanTransitionTo reports whether transitioning from s to next is permitted
// by the task state machine.
func (s TaskStatus) CanTransitionTo(next TaskStatus) bool {
	return ValidateTransition(s, next) == nil
}

// AllowedTransitions returns the list of states reachable from s.
// Returns nil if s has no outgoing transitions (e.g. terminal or unknown state).
func (s TaskStatus) AllowedTransitions() []TaskStatus {
	return allowedTransitions[s]
}

// CurrentTaskSchemaVersion is the on-disk schema version for task.json.
// Increment this constant whenever a new migration step is added to
// migrateTaskJSON so that already-migrated files are not re-written on
// every startup.
const CurrentTaskSchemaVersion = 1

// DefaultRetryHistoryLimit, DefaultRefineSessionsLimit, and
// DefaultPromptHistoryLimit cap the number of entries persisted for the three
// unboundedly-growing task slice fields. Each limit can be overridden via the
// corresponding WALLFACER_*_LIMIT environment variable. A value of 0 disables
// pruning for that field.
const (
	DefaultRetryHistoryLimit   = 10
	DefaultRefineSessionsLimit = 5
	DefaultPromptHistoryLimit  = 20
)

// PayloadLimits holds the effective pruning limits for the three
// unboundedly-growing task slice fields. Values are exposed via GET /api/config
// so the UI can display "showing last N entries" contextual messages.
type PayloadLimits struct {
	RetryHistory   int `json:"retry_history"`
	RefineSessions int `json:"refine_sessions"`
	PromptHistory  int `json:"prompt_history"`
}

// Task is the core domain model: a unit of work executed by an agent.
type Task struct {
	SchemaVersion     int                 `json:"schema_version"`
	ID                uuid.UUID           `json:"id"`
	Title             string              `json:"title,omitempty"`
	Prompt            string              `json:"prompt"`
	PromptHistory     []string            `json:"prompt_history,omitempty"`
	RetryHistory      []RetryRecord       `json:"retry_history,omitempty"`
	RefineSessions    []RefinementSession `json:"refine_sessions,omitempty"`
	CurrentRefinement *RefinementJob      `json:"current_refinement,omitempty"`
	Status            TaskStatus          `json:"status"`
	Archived          bool                `json:"archived,omitempty"`
	SessionID         *string             `json:"session_id"`
	FreshStart        bool                `json:"fresh_start,omitempty"`
	Result            *string             `json:"result"`
	StopReason        *string             `json:"stop_reason"`
	Turns             int                 `json:"turns"`
	Timeout           int                 `json:"timeout"`
	MaxCostUSD        float64             `json:"max_cost_usd,omitempty"`     // 0 = unlimited
	MaxInputTokens    int                 `json:"max_input_tokens,omitempty"` // 0 = unlimited; counts input+cache_read+cache_creation
	Usage             TaskUsage           `json:"usage"`
	Sandbox           string              `json:"sandbox,omitempty"`
	SandboxByActivity map[string]string   `json:"sandbox_by_activity,omitempty"`
	// UsageBreakdown tracks token/cost per sub-agent activity (e.g. "implementation",
	// "test", "title", "oversight", "oversight-test", "refinement").
	UsageBreakdown map[string]TaskUsage `json:"usage_breakdown,omitempty"`
	// Environment records the runtime environment captured at the start of execution.
	Environment *ExecutionEnvironment `json:"environment,omitempty"`
	Position    int                   `json:"position"`
	CreatedAt   time.Time             `json:"created_at"`
	UpdatedAt   time.Time             `json:"updated_at"`

	// Worktree isolation fields (populated when task moves to in_progress).
	WorktreePaths    map[string]string `json:"worktree_paths,omitempty"`     // host repoPath → worktree path
	BranchName       string            `json:"branch_name,omitempty"`        // "task/<uuid8>"
	CommitHashes     map[string]string `json:"commit_hashes,omitempty"`      // host repoPath → commit hash after merge
	BaseCommitHashes map[string]string `json:"base_commit_hashes,omitempty"` // host repoPath → defBranch HEAD before merge
	MountWorktrees   bool              `json:"mount_worktrees,omitempty"`
	Model            string            `json:"model,omitempty"`          // deprecated: retained for migration compatibility
	ModelOverride    *string           `json:"model_override,omitempty"` // per-task model override; nil means use global default

	// Test verification fields.
	IsTestRun        bool   `json:"is_test_run,omitempty"`         // true while the task is running as a test verifier
	LastTestResult   string `json:"last_test_result,omitempty"`    // "pass", "fail", "unknown" (tested, no clear verdict), or "" (not yet tested)
	TestRunStartTurn int    `json:"test_run_start_turn,omitempty"` // turn count when the test run started (implementation turn boundary)

	// Kind identifies the execution mode (TaskKindTask or TaskKindIdeaAgent).
	// Empty string and "task" are equivalent: a standard implementation task.
	Kind TaskKind `json:"kind,omitempty"`

	// Tags are labels attached to a task for categorisation (e.g. "idea-agent" for
	// tasks auto-created by the brainstorm agent).
	Tags []string `json:"tags,omitempty"`

	// ExecutionPrompt overrides Prompt when the sandbox agent is invoked.
	// When set, the runner passes ExecutionPrompt to the container instead of
	// Prompt, keeping Prompt as the short human-readable card label (typically
	// just the task title for idea-tagged cards). Empty means use Prompt.
	ExecutionPrompt string `json:"execution_prompt,omitempty"`

	// DependsOn lists UUIDs of tasks that must all reach TaskStatusDone
	// before this task is eligible for auto-promotion.
	// Nil/empty means no dependencies (backward-compatible default).
	DependsOn []string `json:"depends_on,omitempty"`

	// ScheduledAt is an optional future time before which the task will not
	// be auto-promoted from backlog. Nil means "run as soon as there is
	// capacity" (the existing default behaviour).
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`

	// ForkedFrom is the UUID of the source task this task was branched from, if any.
	ForkedFrom *uuid.UUID `json:"forked_from,omitempty"`
}

// HasTag reports whether the task has the given tag.
func (t *Task) HasTag(tag string) bool {
	for _, v := range t.Tags {
		if v == tag {
			return true
		}
	}
	return false
}

// deepCloneTask returns a full copy of t, duplicating all slices, maps, and
// pointer-bearing fields so callers can safely mutate the returned value.
func deepCloneTask(t *Task) Task {
	if t == nil {
		return Task{}
	}

	cp := *t
	cp.PromptHistory = slices.Clone(t.PromptHistory)
	cp.RetryHistory = slices.Clone(t.RetryHistory)
	cp.RefineSessions = cloneRefinementSessions(t.RefineSessions)
	cp.Tags = slices.Clone(t.Tags)
	cp.DependsOn = slices.Clone(t.DependsOn)
	cp.SandboxByActivity = maps.Clone(t.SandboxByActivity)
	cp.UsageBreakdown = maps.Clone(t.UsageBreakdown)
	cp.WorktreePaths = maps.Clone(t.WorktreePaths)
	cp.CommitHashes = maps.Clone(t.CommitHashes)
	cp.BaseCommitHashes = maps.Clone(t.BaseCommitHashes)

	if t.CurrentRefinement != nil {
		jobCopy := *t.CurrentRefinement
		cp.CurrentRefinement = &jobCopy
	}
	if t.SessionID != nil {
		sessionID := *t.SessionID
		cp.SessionID = &sessionID
	}
	if t.Result != nil {
		result := *t.Result
		cp.Result = &result
	}
	if t.StopReason != nil {
		stopReason := *t.StopReason
		cp.StopReason = &stopReason
	}
	if t.Environment != nil {
		envCopy := *t.Environment
		cp.Environment = &envCopy
	}
	if t.ScheduledAt != nil {
		scheduledAt := *t.ScheduledAt
		cp.ScheduledAt = &scheduledAt
	}
	if t.ForkedFrom != nil {
		forkedFrom := *t.ForkedFrom
		cp.ForkedFrom = &forkedFrom
	}
	if t.ModelOverride != nil {
		modelOverride := *t.ModelOverride
		cp.ModelOverride = &modelOverride
	}

	return cp
}

func cloneRefinementSessions(src []RefinementSession) []RefinementSession {
	if src == nil {
		return nil
	}

	dst := make([]RefinementSession, len(src))
	for i := range src {
		dst[i] = src[i]
		dst[i].Messages = slices.Clone(src[i].Messages)
	}
	return dst
}

// Tombstone records when and why a task was soft-deleted.
// A tombstone.json file in the task directory marks the task as deleted but
// retains all data on disk until the retention period expires.
type Tombstone struct {
	DeletedAt time.Time `json:"deleted_at"`
	Reason    string    `json:"reason,omitempty"`
}

// TaskSummary is an immutable snapshot written exactly once when a task
// transitions to TaskStatusDone. It captures the final cost, usage breakdown,
// and key metadata so that analytics endpoints can avoid re-reading the full
// task.json for completed tasks.
type TaskSummary struct {
	TaskID          uuid.UUID            `json:"task_id"`
	Title           string               `json:"title"`
	Status          TaskStatus           `json:"status"`
	CompletedAt     time.Time            `json:"completed_at"`
	CreatedAt       time.Time            `json:"created_at"`
	DurationSeconds float64              `json:"duration_seconds"`
	TotalTurns      int                  `json:"total_turns"`
	TotalCostUSD    float64              `json:"total_cost_usd"`
	ByActivity      map[string]TaskUsage `json:"by_activity"`
	TestResult      string               `json:"test_result"`
	PhaseCount      int                  `json:"phase_count"`
}

// TaskSearchResult wraps a Task with search match metadata.
type TaskSearchResult struct {
	*Task
	MatchedField string `json:"matched_field"` // "title" | "prompt" | "tags" | "oversight"
	Snippet      string `json:"snippet"`       // HTML-escaped context around the match
}

// OversightStatus represents the generation state of a task's aggregated oversight summary.
type OversightStatus string

const (
	OversightStatusPending    OversightStatus = "pending"
	OversightStatusGenerating OversightStatus = "generating"
	OversightStatusReady      OversightStatus = "ready"
	OversightStatusFailed     OversightStatus = "failed"
)

// OversightPhase is a logical grouping of related agent activities within a task run.
type OversightPhase struct {
	Timestamp time.Time `json:"timestamp"`
	Title     string    `json:"title"`
	Summary   string    `json:"summary"`
	ToolsUsed []string  `json:"tools_used,omitempty"`
	Commands  []string  `json:"commands,omitempty"`
	Actions   []string  `json:"actions,omitempty"`
}

// TaskOversight holds the aggregated high-level summary of a task's agent execution trace.
// It is generated asynchronously when a task transitions to waiting, done, or failed.
type TaskOversight struct {
	Status      OversightStatus  `json:"status"`
	GeneratedAt time.Time        `json:"generated_at,omitempty"`
	Error       string           `json:"error,omitempty"`
	Phases      []OversightPhase `json:"phases,omitempty"`
}

// EventType identifies the kind of event stored in a task's audit trail.
type EventType string

const (
	EventTypeStateChange EventType = "state_change"
	EventTypeOutput      EventType = "output"
	EventTypeFeedback    EventType = "feedback"
	EventTypeError       EventType = "error"
	EventTypeSystem      EventType = "system"
	EventTypeSpanStart   EventType = "span_start"
	EventTypeSpanEnd     EventType = "span_end"
)

// Trigger constants identify what caused a state_change event.
const (
	TriggerUser        = "user"
	TriggerAutoPromote = "auto_promote"
	TriggerAutoTest    = "auto_test"
	TriggerAutoSubmit  = "auto_submit"
	TriggerFeedback    = "feedback"
	TriggerSync        = "sync"
	TriggerRecovery    = "recovery"
	TriggerSystem      = "system"
)

// SpanData holds metadata for a span_start or span_end event.
// Phase identifies the execution phase (e.g. "worktree_setup", "agent_turn",
// "container_run", "commit"). Label allows differentiating multiple spans of
// the same phase (e.g. "agent_turn_1", "agent_turn_2").
type SpanData struct {
	Phase string `json:"phase"`
	Label string `json:"label"`
}

// TaskEvent is a single event in a task's audit trail (event sourcing).
type TaskEvent struct {
	ID        int64           `json:"id"`
	TaskID    uuid.UUID       `json:"task_id"`
	EventType EventType       `json:"event_type"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
}
