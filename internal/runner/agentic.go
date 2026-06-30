package runner

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"latere.ai/x/wallfacer/internal/agentgraph"
	"latere.ai/x/wallfacer/internal/constants"
	"latere.ai/x/wallfacer/internal/envconfig"
	"latere.ai/x/wallfacer/internal/flow"
	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/store"
)

// agenticModelConfig derives the model selection for an agentic run from
// wallfacer's global credential settings (the .env file), the same source the
// container harnesses read. It mirrors the guard the other env-reading runner
// helpers use: a missing or unparseable env file, or an absent
// ANTHROPIC_API_KEY, yields the zero ModelConfig, which the agentgraph seam maps
// to the deterministic fake model (so tests and no-credential dev keep working).
// A configured ANTHROPIC_BASE_URL routes through Lux (the gateway); a bare key
// talks to the provider directly. Only the static x-api-key credential is wired
// for now; Bearer-style tokens (ANTHROPIC_AUTH_TOKEN, CLAUDE_CODE_OAUTH_TOKEN)
// are deferred (they need a per-call BearerSource).
func (r *Runner) agenticModelConfig() agentgraph.ModelConfig {
	if r.envFile == "" {
		return agentgraph.ModelConfig{}
	}
	cfg, err := envconfig.Parse(r.envFile)
	if err != nil {
		return agentgraph.ModelConfig{}
	}
	if cfg.APIKey == "" {
		return agentgraph.ModelConfig{}
	}
	mode := agentgraph.ModelModeDirect
	if cfg.BaseURL != "" {
		mode = agentgraph.ModelModeLux
	}
	return agentgraph.ModelConfig{
		Mode:     mode,
		Provider: "anthropic",
		Model:    cfg.DefaultModel,
		BaseURL:  cfg.BaseURL,
		APIKey:   cfg.APIKey,
	}
}

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

// agenticTraceEvent maps a topos trace event to a task-timeline event, returning
// ok=false for events that should not surface (lifecycle bookkeeping, empty
// payloads). It renders the agent-graph run as a readable live trace: each agent
// turn's assistant text, delegations, and tool use. The agent label is the
// lineage node id (ev.Node) so the timeline lines join to graph nodes.
func agenticTraceEvent(ev agentgraph.TraceEvent) (store.EventType, map[string]string, bool) {
	label := ev.AgentID
	if label == "" {
		label = ev.Node
	}
	// trace builds the timeline data: a human "result" line (so the events tab
	// reads naturally) plus structured fields the Agent Graph view groups on
	// (source marks it as an agent-graph trace; node is the lineage join key).
	trace := func(kind, result, text string) (store.EventType, map[string]string, bool) {
		return store.EventTypeSystem, map[string]string{
			"result": result,
			"source": "agentgraph",
			"kind":   kind,
			"node":   ev.Node,
			"agent":  label,
			"text":   text,
		}, true
	}
	switch ev.Name {
	case "AssistantMessage":
		var p struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(ev.PayloadJSON, &p)
		if p.Text == "" {
			return "", nil, false
		}
		return trace("assistant", label+": "+p.Text, p.Text)
	case "SubagentStart":
		return trace("delegate", "↳ delegated to "+label, "")
	case "PostToolUse":
		var p struct {
			ToolCall struct {
				Name string `json:"name"`
			} `json:"tool_call"`
		}
		_ = json.Unmarshal(ev.PayloadJSON, &p)
		if p.ToolCall.Name == "" {
			return "", nil, false
		}
		return trace("tool", label+" used "+p.ToolCall.Name, p.ToolCall.Name)
	default:
		return "", nil, false
	}
}

// runAgenticFlow executes an agentic flow through the in-process topos
// agent-graph runtime (internal/agentgraph): it compiles the flow into a
// multi-agent topos.Region and drives the resulting run onto the task via
// driveToposRun. The caller sets statusSet=true before invoking this.
func (r *Runner) runAgenticFlow(bgCtx context.Context, taskID uuid.UUID, task store.Task, f flow.Flow, prompt string) {
	r.driveToposRun(bgCtx, taskID, task, func(ctx context.Context, onEvent func(agentgraph.TraceEvent)) (agentgraph.Result, error) {
		return agentgraph.RunFlowWithModel(ctx, task.ID.String(), r.agenticModelConfig(), f, r.agentsReg, prompt, onEvent)
	})
}

// runNativeTopos executes a plain task through the native Topos harness: a single
// in-process agent (a one-node topos region) rather than a multi-agent flow. It
// is the path a task resolves to when its harness is Topos (the native default,
// once harness.Default() is flipped; until then only an explicit topos pin
// reaches here). Like the agentic-flow path it produces a final text + lineage
// and walks the state machine, but it does not yet make a durable git commit or
// run verification — commit/verify parity with the subprocess harnesses is
// tracked in the topos-native-harness spec.
func (r *Runner) runNativeTopos(bgCtx context.Context, taskID uuid.UUID, task store.Task, prompt string) {
	r.driveToposRun(bgCtx, taskID, task, func(ctx context.Context, onEvent func(agentgraph.TraceEvent)) (agentgraph.Result, error) {
		return agentgraph.RunAgent(ctx, task.ID.String(), r.agenticModelConfig(), "implement", "", prompt, onEvent)
	})
}

// driveToposRun executes an in-process topos run (a multi-agent agentic flow or a
// single-agent native-harness run) supplied as runFn, and maps the outcome onto
// the task. It forwards the run's live trace events onto the task timeline (so
// the per-turn assistant text, delegations, and tool use are visible as the run
// proceeds, not just as a lineage graph at the end), persists the final text and
// the JSON-marshalled lineage graph, then walks the task through the same
// in_progress -> waiting -> committing -> done state machine the flow-engine and
// ideation branches use (the state machine forbids a direct in_progress -> done
// transition). runFn performs the actual topos run, wired with the supplied
// non-blocking observer. The caller sets statusSet=true before invoking this.
func (r *Runner) driveToposRun(bgCtx context.Context, taskID uuid.UUID, task store.Task, runFn func(ctx context.Context, onEvent func(agentgraph.TraceEvent)) (agentgraph.Result, error)) {
	timeout := time.Duration(task.Timeout) * time.Minute
	if timeout <= 0 {
		timeout = constants.DefaultTaskTimeout
	}
	ctx, cancel := context.WithTimeout(bgCtx, timeout)
	defer cancel()

	// Forward the topos run's live events onto the task timeline. The topos
	// observer is called synchronously on the run goroutine(s), so it must not
	// block: push to a buffered channel and drain into the store from a separate
	// goroutine (dropping on overflow rather than backpressuring the run).
	traceCh := make(chan agentgraph.TraceEvent, 256)
	traceDone := make(chan struct{})
	go func() {
		defer close(traceDone)
		for ev := range traceCh {
			etype, data, ok := agenticTraceEvent(ev)
			if !ok {
				continue
			}
			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, etype, data)
		}
	}()
	onEvent := func(ev agentgraph.TraceEvent) {
		select {
		case traceCh <- ev:
		default: // buffer full: drop rather than stall the run
		}
	}

	res, err := runFn(ctx, onEvent)
	close(traceCh)
	<-traceDone
	if err != nil {
		if cur, _ := r.taskStore(taskID).GetTask(bgCtx, taskID); cur != nil && cur.Status == store.TaskStatusCancelled {
			return
		}
		logger.Runner.Error("topos run", "task", taskID, "error", err)
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
