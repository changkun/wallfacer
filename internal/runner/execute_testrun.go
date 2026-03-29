package runner

import (
	"context"
	"strings"

	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
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
	// is detectable, default to "fail" to prevent auto-submit from promoting
	// a potentially broken task to done without human review.
	verdict := parseTestVerdict(result, task.CustomPassPatterns, task.CustomFailPatterns)
	if verdict == "" {
		verdict = "fail"
	}
	_ = r.taskStore(taskID).UpdateTaskTestRun(ctx, taskID, false, verdict)

	if verdict == "fail" {
		_ = r.taskStore(taskID).IncrementTestFailCount(ctx, taskID)
		_ = r.taskStore(taskID).UpdateTaskPendingTestFeedback(ctx, taskID, buildTestFailureFeedback(result))
	} else {
		_ = r.taskStore(taskID).ResetTestFailCount(ctx, taskID)
	}

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
