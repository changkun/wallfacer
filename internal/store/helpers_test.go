package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/sandbox"
)

// bg returns a background context for use in tests.
func bg() context.Context {
	return context.Background()
}

// newTestStore creates a Store backed by a fresh temporary directory.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

// setTaskCloneFixture populates all nested, pointer, slice, and map fields on
// task with deterministic values, then returns a deep clone. The clone serves
// as the "golden snapshot" that later assertions compare against to verify
// isolation of store-returned copies.
func setTaskCloneFixture(t *testing.T, task *Task) Task {
	t.Helper()

	sessionID := "session-1"
	result := "result-1"
	stopReason := "stop-1"
	scheduledAt := time.Unix(1_700_000_000, 0).UTC()

	messageTime := time.Unix(1_700_000_010, 0).UTC()

	task.PromptHistory = []string{"prompt-old"}
	task.RetryHistory = []RetryRecord{{
		RetiredAt: time.Unix(1_700_000_020, 0).UTC(),
		Prompt:    "retry-prompt",
		Status:    TaskStatusFailed,
		Result:    "retry-result",
		SessionID: "retry-session",
		Turns:     3,
		CostUSD:   1.25,
	}}
	task.RefineSessions = []RefinementSession{{
		ID:          "refine-1",
		CreatedAt:   time.Unix(1_700_000_030, 0).UTC(),
		StartPrompt: "refine-start",
		Messages: []RefinementMessage{{
			Role:      "assistant",
			Content:   "refine-message",
			CreatedAt: messageTime,
		}},
		Result:       "refine-result",
		ResultPrompt: "refine-applied",
	}}
	task.CurrentRefinement = &RefinementJob{
		ID:        "job-1",
		CreatedAt: time.Unix(1_700_000_040, 0).UTC(),
		Status:    "running",
		Result:    "job-result",
		Error:     "job-error",
		Source:    "runner",
	}
	task.SessionID = &sessionID
	task.Result = &result
	task.StopReason = &stopReason
	task.SandboxByActivity = map[SandboxActivity]sandbox.Type{
		SandboxActivityImplementation: sandbox.Claude,
	}
	task.UsageBreakdown = map[SandboxActivity]TaskUsage{
		SandboxActivityImplementation: {
			InputTokens:          7,
			OutputTokens:         11,
			CacheReadInputTokens: 13,
			CacheCreationTokens:  17,
			CostUSD:              2.5,
		},
	}
	task.Environment = &ExecutionEnvironment{
		ContainerImage:   "image:latest",
		ContainerDigest:  "sha256:digest",
		ModelName:        "model-1",
		APIBaseURL:       "https://example.invalid",
		InstructionsHash: "hash-1",
		RecordedAt:       time.Unix(1_700_000_050, 0).UTC(),
	}
	task.WorktreePaths = map[string]string{"/repo": "/worktree"}
	task.CommitHashes = map[string]string{"/repo": "commit-1"}
	task.BaseCommitHashes = map[string]string{"/repo": "base-1"}
	task.Tags = []string{"tag-1"}
	task.DependsOn = []string{"dep-1"}
	task.ScheduledAt = &scheduledAt

	return deepCloneTask(task)
}

// mutateTaskCloneForIsolation modifies every nested field on task in-place.
// After calling this on a clone returned by GetTask/ListTasks, a subsequent
// read from the store should still match the original snapshot -- proving the
// clone is fully independent of store-owned state.
func mutateTaskCloneForIsolation(task *Task) {
	task.PromptHistory[0] = "mutated-prompt-history"
	task.RetryHistory[0].Prompt = "mutated-retry-prompt"
	task.RefineSessions[0].Messages[0].Content = "mutated-refine-message"
	task.Tags[0] = "mutated-tag"
	task.DependsOn[0] = "mutated-dependency"
	task.SandboxByActivity[SandboxActivityImplementation] = "mutated-sandbox"
	task.UsageBreakdown[SandboxActivityImplementation] = TaskUsage{InputTokens: 999}
	task.Environment.ContainerImage = "mutated-image"
	task.WorktreePaths["/repo"] = "/mutated-worktree"
	task.CommitHashes["/repo"] = "mutated-commit"
	task.BaseCommitHashes["/repo"] = "mutated-base"
	task.CurrentRefinement.Result = "mutated-job-result"
	*task.SessionID = "mutated-session"
	*task.Result = "mutated-result"
	*task.StopReason = "mutated-stop"
	*task.ScheduledAt = time.Unix(1_800_000_000, 0).UTC()
}

// assertTaskMatchesSnapshot fails the test if got does not deeply equal want.
// Used to verify that store-returned clones are not aliased with internal state.
func assertTaskMatchesSnapshot(t *testing.T, got *Task, want Task) {
	t.Helper()
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("task mismatch after clone mutation\n got: %#v\nwant: %#v", *got, want)
	}
}
