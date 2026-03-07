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
type RefinementMessage struct {
	Role      string    `json:"role"`       // "user" or "assistant"
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// RefinementSession records a single chat-based refinement interaction.
// StartPrompt is the task prompt at the beginning of the session.
// ResultPrompt is the prompt after the user applied the refinement (empty if discarded).
type RefinementSession struct {
	ID           string              `json:"id"`
	CreatedAt    time.Time           `json:"created_at"`
	StartPrompt  string              `json:"start_prompt"`
	Messages     []RefinementMessage `json:"messages"`
	ResultPrompt string              `json:"result_prompt,omitempty"`
}

// Task is the core domain model: a unit of work executed by Claude Code.
type Task struct {
	ID             uuid.UUID           `json:"id"`
	Title          string              `json:"title,omitempty"`
	Prompt         string              `json:"prompt"`
	PromptHistory  []string            `json:"prompt_history,omitempty"`
	RefineSessions []RefinementSession `json:"refine_sessions,omitempty"`
	Status        string    `json:"status"`
	Archived      bool      `json:"archived,omitempty"`
	SessionID     *string   `json:"session_id"`
	FreshStart    bool      `json:"fresh_start,omitempty"`
	Result        *string   `json:"result"`
	StopReason    *string   `json:"stop_reason"`
	Turns         int       `json:"turns"`
	Timeout       int       `json:"timeout"`
	Usage         TaskUsage `json:"usage"`
	Position      int       `json:"position"`
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
	IsTestRun      bool   `json:"is_test_run,omitempty"`       // true while the task is running as a test verifier
	LastTestResult string `json:"last_test_result,omitempty"`  // "pass", "fail", or "" (not yet tested)
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
