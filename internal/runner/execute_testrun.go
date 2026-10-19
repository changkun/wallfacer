package runner

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/store"
)

// finalizeTestRun handles the common path for a completed test-run agent turn:
// it parses the verdict, updates test state, generates synchronous oversight,
// moves the task to Waiting, and emits the standard event sequence.
// It must be called without holding any store lock.
func (r *Runner) finalizeTestRun(
	ctx context.Context,
	taskID uuid.UUID,
	task store.Task,
	result string,
) {
	// Parse the test agent's output to determine pass/fail. When no verdict
	// is detectable, default to "fail" so a possibly broken task is not shown
	// as passing.
	verdict := parseTestVerdict(result, task.CustomPassPatterns, task.CustomFailPatterns)
	if verdict == "" {
		verdict = "fail"
	}
	_ = r.taskStore(taskID).UpdateTaskTestRun(ctx, taskID, false, verdict)

	// GenerateTestOversight is synchronous: oversight must be in terminal
	// state before the task becomes visible as 'waiting'.
	r.GenerateTestOversight(taskID, task.TestRunStartTurn)

	_ = r.taskStore(taskID).InsertEvent(ctx, taskID, store.EventTypeSystem, map[string]string{
		"result": "Test verification complete: " + strings.ToUpper(verdict),
	})
	_ = r.taskStore(taskID).UpdateTaskStatus(ctx, taskID, store.TaskStatusWaiting)
	_ = r.taskStore(taskID).InsertEvent(ctx, taskID, store.EventTypeStateChange,
		store.NewStateChangeData(store.TaskStatusInProgress, store.TaskStatusWaiting, store.TriggerSystem, nil))
	_ = r.taskStore(taskID).InsertEvent(ctx, taskID, store.EventTypeSpanStart,
		store.SpanData{Phase: "feedback_waiting", Label: "feedback_waiting"})
}
