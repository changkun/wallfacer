package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"changkun.de/wallfacer/internal/sandbox"
	"github.com/google/uuid"
)

// bg returns a background context for use in tests.
func bg() context.Context {
	return context.Background()
}

// newTestStore creates a Store backed by a fresh temporary directory.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func setTaskCloneFixture(t *testing.T, task *Task) Task {
	t.Helper()

	sessionID := "session-1"
	result := "result-1"
	stopReason := "stop-1"
	scheduledAt := time.Unix(1_700_000_000, 0).UTC()
	forkedFrom := uuid.MustParse("11111111-1111-1111-1111-111111111111")
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
	task.SandboxByActivity = map[string]sandbox.Type{
		SandboxActivityImplementation: sandbox.Claude,
	}
	task.UsageBreakdown = map[string]TaskUsage{
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
	task.ForkedFrom = &forkedFrom

	return deepCloneTask(task)
}

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
	*task.ForkedFrom = uuid.MustParse("22222222-2222-2222-2222-222222222222")
}

func assertTaskMatchesSnapshot(t *testing.T, got *Task, want Task) {
	t.Helper()
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("task mismatch after clone mutation\n got: %#v\nwant: %#v", *got, want)
	}
}
