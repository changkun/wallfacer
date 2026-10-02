package runner

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/store"
)

// removedFleetEvents returns the data of every removed-fleet system event on
// the task's timeline.
func removedFleetEvents(t *testing.T, s *store.Store, taskID uuid.UUID) []map[string]string {
	t.Helper()
	events, err := s.GetEvents(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	var out []map[string]string
	for _, ev := range events {
		if ev.EventType != store.EventTypeSystem {
			continue
		}
		var data map[string]string
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			continue // system events with non-string payloads are not ours
		}
		if data["kind"] == removedFleetEventKind {
			out = append(out, data)
		}
	}
	return out
}

// TestRun_StoredFlowIDRunsBuiltinPipelineWithNotice covers a task record
// written while fleets existed: its FlowID names a fleet that no longer
// exists. The run takes the built-in pipeline (the subprocess turn loop runs
// and the task stops in waiting for review), and the timeline carries one
// system event naming the fleet. A second run of the same task, resumed from
// waiting, adds no second notice.
func TestRun_StoredFlowIDRunsBuiltinPipelineWithNotice(t *testing.T) {
	repo := setupTestRepo(t)
	cmd := fakeCmdScript(t, endTurnOutput, 0)
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt:  "work named by a removed fleet",
		Timeout: 5,
		FlowID:  "review-fleet",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	r.Run(task.ID, task.Prompt, "", false)

	updated, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if updated.Status != store.TaskStatusWaiting {
		t.Fatalf("status = %q, want waiting (the built-in pipeline stops for review)", updated.Status)
	}
	if updated.Result == nil || *updated.Result != "task complete" {
		t.Fatalf("result = %v, want the turn loop's output", updated.Result)
	}
	if updated.FlowID != "review-fleet" {
		t.Errorf("FlowID = %q, want the stored value kept as a record field", updated.FlowID)
	}

	notices := removedFleetEvents(t, s, task.ID)
	if len(notices) != 1 {
		t.Fatalf("removed-fleet events = %v, want exactly one", notices)
	}
	if notices[0]["flow_id"] != "review-fleet" {
		t.Errorf("notice flow_id = %q, want review-fleet", notices[0]["flow_id"])
	}
	if want := `The fleet "review-fleet" this task names no longer exists, so the task runs on the built-in pipeline.`; notices[0]["result"] != want {
		t.Errorf("notice result = %q, want %q", notices[0]["result"], want)
	}

	// Resume the task from waiting: the second run writes no second notice.
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus (resume): %v", err)
	}
	r.Run(task.ID, "continue", "sess1", true)
	if got := removedFleetEvents(t, s, task.ID); len(got) != 1 {
		t.Fatalf("removed-fleet events after a resumed run = %v, want still exactly one", got)
	}
}

// TestRun_NoRemovedFleetNoticeForBuiltinPipeline asserts tasks that name no
// fleet, or name the built-in pipeline itself, get no notice.
func TestRun_NoRemovedFleetNoticeForBuiltinPipeline(t *testing.T) {
	repo := setupTestRepo(t)
	cmd := fakeCmdScript(t, endTurnOutput, 0)
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	for _, flowID := range []string{"", builtinPipelineID} {
		task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
			Prompt:  "ordinary work",
			Timeout: 5,
			FlowID:  flowID,
		})
		if err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
		if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
			t.Fatalf("UpdateTaskStatus: %v", err)
		}
		r.Run(task.ID, task.Prompt, "", false)

		updated, err := s.GetTask(ctx, task.ID)
		if err != nil {
			t.Fatalf("GetTask: %v", err)
		}
		if updated.Status != store.TaskStatusWaiting {
			t.Errorf("FlowID %q: status = %q, want waiting", flowID, updated.Status)
		}
		if got := removedFleetEvents(t, s, task.ID); len(got) != 0 {
			t.Errorf("FlowID %q: removed-fleet events = %v, want none", flowID, got)
		}
	}
}
