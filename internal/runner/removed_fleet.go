package runner

import (
	"context"
	"encoding/json"
	"fmt"

	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/store"
)

// builtinPipelineID is the FlowID value a record written for the built-in
// pipeline carries. Any other non-empty value named a user-authored fleet.
const builtinPipelineID = "implement"

// removedFleetEventKind is the kind of the system event that tells a task's
// timeline the fleet its record names no longer exists.
const removedFleetEventKind = "fleet:removed"

// noteRemovedFleet writes one system event on the timeline of a task whose
// FlowID names a fleet other than the built-in pipeline, saying the fleet no
// longer exists and the task runs on the built-in pipeline. FlowID is a
// record field kept from when a task could run on a user-authored fleet: no
// writer sets it and dispatch never reads it, so the event is the only place
// the value still surfaces.
//
// The event is written once per task. A resumed, retried or test run of the
// same task finds the earlier event and writes nothing, so the timeline does
// not repeat the notice on every run. When the timeline cannot be read the
// notice is skipped and logged rather than risk writing a duplicate.
func (r *Runner) noteRemovedFleet(ctx context.Context, task *store.Task) {
	if task.FlowID == "" || task.FlowID == builtinPipelineID {
		return
	}
	s := r.taskStore(task.ID)
	events, err := s.GetEvents(ctx, task.ID)
	if err != nil {
		logger.Runner.Warn("removed fleet notice: read timeline", "task", task.ID, "error", err)
		return
	}
	for _, ev := range events {
		if ev.EventType != store.EventTypeSystem {
			continue
		}
		var data struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(ev.Data, &data) == nil && data.Kind == removedFleetEventKind {
			return
		}
	}
	if err := s.InsertEvent(ctx, task.ID, store.EventTypeSystem, map[string]string{
		"kind":    removedFleetEventKind,
		"flow_id": task.FlowID,
		"result":  fmt.Sprintf("The fleet %q this task names no longer exists, so the task runs on the built-in pipeline.", task.FlowID),
	}); err != nil {
		logger.Runner.Warn("removed fleet notice: write event", "task", task.ID, "error", err)
	}
}
