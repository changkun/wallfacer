package store

import (
	"encoding/json"
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

// RefinementMessage is a single turn in a refinement chat session.
// Kept for backward compatibility with older chat-based refinement sessions.
type RefinementMessage struct {
	Role      string    `json:"role"`       // "user" or "assistant"
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

// RefinementJob tracks the state of an active or recently completed
// sandbox refinement run for a backlog task.
type RefinementJob struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Status    string    `json:"status"` // "running", "done", "failed"
	Result    string    `json:"result,omitempty"`
	Error     string    `json:"error,omitempty"`
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

// Task is the core domain model: a unit of work executed by an agent.
type Task struct {
	ID             uuid.UUID           `json:"id"`
	Title          string              `json:"title,omitempty"`
	Prompt         string              `json:"prompt"`
	PromptHistory  []string            `json:"prompt_history,omitempty"`
	RefineSessions     []RefinementSession `json:"refine_sessions,omitempty"`
	CurrentRefinement  *RefinementJob      `json:"current_refinement,omitempty"`
	Status         TaskStatus           `json:"status"`
	Archived       bool                 `json:"archived,omitempty"`
	SessionID      *string              `json:"session_id"`
	FreshStart     bool                 `json:"fresh_start,omitempty"`
	Result         *string              `json:"result"`
	StopReason     *string              `json:"stop_reason"`
	Turns          int                  `json:"turns"`
	Timeout        int                  `json:"timeout"`
	Usage          TaskUsage            `json:"usage"`
	// UsageBreakdown tracks token/cost per sub-agent activity (e.g. "implementation",
	// "test", "title", "oversight", "oversight-test", "refinement").
	UsageBreakdown map[string]TaskUsage `json:"usage_breakdown,omitempty"`
	Position       int                  `json:"position"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	// Worktree isolation fields (populated when task moves to in_progress).
	WorktreePaths    map[string]string `json:"worktree_paths,omitempty"`     // host repoPath → worktree path
	BranchName       string            `json:"branch_name,omitempty"`        // "task/<uuid8>"
	CommitHashes     map[string]string `json:"commit_hashes,omitempty"`      // host repoPath → commit hash after merge
	BaseCommitHashes map[string]string `json:"base_commit_hashes,omitempty"` // host repoPath → defBranch HEAD before merge
	MountWorktrees   bool              `json:"mount_worktrees,omitempty"`
	Model            string            `json:"model,omitempty"`

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
)

// TaskEvent is a single event in a task's audit trail (event sourcing).
type TaskEvent struct {
	ID        int64           `json:"id"`
	TaskID    uuid.UUID       `json:"task_id"`
	EventType EventType       `json:"event_type"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
}
