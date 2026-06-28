package runner

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"latere.ai/x/wallfacer/internal/agentgraph"
	"latere.ai/x/wallfacer/internal/constants"
	"latere.ai/x/wallfacer/internal/flow"
	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/store"
)

// flowBySlug looks up a flow by slug, guarding against a nil flow registry
// (hand-constructed Runners in tests may leave it unset). Returning ok=false for
// nil keeps the dispatch falling through to the legacy paths exactly as it did
// before the agentic branch existed.
func (r *Runner) flowBySlug(slug string) (flow.Flow, bool) {
	if r.flows == nil {
		return flow.Flow{}, false
	}
	return r.flows.Get(slug)
}

// runAgenticFlow executes an agentic flow through the in-process topos
// agent-graph runtime (internal/agentgraph) and maps the result back onto the
// task. It compiles the flow into a topos.Region, runs it with the deterministic
// fake model (real Lux model + sandbox wiring is M4), persists the final text
// and the JSON-marshalled lineage graph, then drives the task through the same
// in_progress -> waiting -> committing -> done state machine the flow-engine and
// ideation branches use (the state machine forbids a direct in_progress -> done
// transition). The caller sets statusSet=true before invoking this.
func (r *Runner) runAgenticFlow(bgCtx context.Context, taskID uuid.UUID, task store.Task, f flow.Flow, prompt string) {
	timeout := time.Duration(task.Timeout) * time.Minute
	if timeout <= 0 {
		timeout = constants.DefaultTaskTimeout
	}
	ctx, cancel := context.WithTimeout(bgCtx, timeout)
	defer cancel()

	res, err := agentgraph.RunFlowFake(ctx, task.ID.String(), f, r.agentsReg, prompt)
	if err != nil {
		if cur, _ := r.taskStore(taskID).GetTask(bgCtx, taskID); cur != nil && cur.Status == store.TaskStatusCancelled {
			return
		}
		logger.Runner.Error("agentic flow run", "task", taskID, "flow", f.Slug, "error", err)
		category := classifyFailure(err, false, "")
		_ = r.taskStore(taskID).SetTaskFailureCategory(bgCtx, taskID, category)
		if r.tryAutoRetry(bgCtx, taskID, category) {
			return
		}
		_ = r.taskStore(taskID).UpdateTaskStatus(bgCtx, taskID, store.TaskStatusFailed)
		_ = r.taskStore(taskID).UpdateTaskResult(bgCtx, taskID, err.Error(), "", "", 0)
		_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeError, map[string]string{"error": err.Error()})
		_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeStateChange,
			store.NewStateChangeData(store.TaskStatusInProgress, store.TaskStatusFailed, store.TriggerSystem, nil))
		return
	}

	// Persist the result and lineage before transitioning so the durable record
	// is complete the moment the task reaches done.
	_ = r.taskStore(taskID).UpdateTaskResult(bgCtx, taskID, res.Final, "", "end_turn", 0)
	if data, mErr := json.Marshal(res.Lineage); mErr == nil {
		if lErr := r.taskStore(taskID).UpdateTaskLineage(bgCtx, taskID, string(data)); lErr != nil {
			logger.Runner.Warn("agentic flow lineage persist", "task", taskID, "error", lErr)
		}
	} else {
		logger.Runner.Warn("agentic flow lineage marshal", "task", taskID, "error", mErr)
	}
	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeOutput, map[string]string{
		"result": res.Final,
	})

	_ = r.taskStore(taskID).UpdateTaskStatus(bgCtx, taskID, store.TaskStatusWaiting)
	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeStateChange,
		store.NewStateChangeData(store.TaskStatusInProgress, store.TaskStatusWaiting, store.TriggerSystem, nil))
	_ = r.taskStore(taskID).UpdateTaskStatus(bgCtx, taskID, store.TaskStatusCommitting)
	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeStateChange,
		store.NewStateChangeData(store.TaskStatusWaiting, store.TaskStatusCommitting, store.TriggerSystem, nil))
	_ = r.taskStore(taskID).UpdateTaskStatus(bgCtx, taskID, store.TaskStatusDone)
	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeStateChange,
		store.NewStateChangeData(store.TaskStatusCommitting, store.TaskStatusDone, store.TriggerSystem, nil))
}
