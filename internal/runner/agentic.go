package runner

import (
	"context"
	"encoding/json"
	"net/url"
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
	baseURL := ""
	if cfg.BaseURL != "" {
		mode = agentgraph.ModelModeLux
		baseURL = gatewayOrigin(cfg.BaseURL)
	}
	return agentgraph.ModelConfig{
		Mode:     mode,
		Provider: "anthropic",
		Model:    cfg.DefaultModel,
		BaseURL:  baseURL,
		APIKey:   cfg.APIKey,
	}
}

// gatewayOrigin reduces the .env's ANTHROPIC_BASE_URL, which is shaped for the
// container harness (Claude Code dials the gateway's anthropic-wire surface,
// e.g. https://lux.latere.ai/anthropic), to the origin the lux-native dialect
// (POST /lux/v1/generate) lives under. The .env stays harness-shaped; the model
// leg derives the base it needs. An unparseable value passes through untouched
// so the resulting request error names the configured URL.
func gatewayOrigin(harnessBase string) string {
	u, err := url.Parse(harnessBase)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return harnessBase
	}
	return u.Scheme + "://" + u.Host
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
// and walks the state machine; driveToposRun then runs the real commit pipeline
// so the worktree edits land as a durable git commit. Verification (the test
// step) parity with the subprocess harnesses is tracked in the
// topos-native-harness spec.
func (r *Runner) runNativeTopos(bgCtx context.Context, taskID uuid.UUID, task store.Task, prompt, worktree string) {
	// worktree is the task's set-up worktree (the real repo) so the agent's tools
	// edit actual files; an empty worktree falls back to the topos temp-dir sandbox.
	r.driveToposRun(bgCtx, taskID, task, func(ctx context.Context, onEvent func(agentgraph.TraceEvent)) (agentgraph.Result, error) {
		return agentgraph.RunAgent(ctx, task.ID.String(), r.agenticModelConfig(), "implement", "", prompt, worktree, onEvent)
	})
}

// firstWorktreePath returns one worktree path from the map (the primary repo's
// worktree), or "" when none is set up. WorktreePaths is keyed by host repo path;
// a single-repo task has one entry.
func firstWorktreePath(worktreePaths map[string]string) string {
	for _, wt := range worktreePaths {
		return wt
	}
	return ""
}

// driveToposRun executes an in-process topos run (a multi-agent agentic flow or a
// single-agent native-harness run) supplied as runFn, and maps the outcome onto
// the task. It forwards the run's live trace events onto the task timeline (so
// the per-turn assistant text, delegations, and tool use are visible as the run
// proceeds, not just as a lineage graph at the end), persists the final text and
// the JSON-marshalled lineage graph, then walks the task through the same
// in_progress -> waiting -> committing -> done state machine the flow-engine and
// ideation branches use (the state machine forbids a direct in_progress -> done
// transition). In the committing phase it runs the real commit pipeline
// (r.Commit) when the run has a worktree, so a native run's edits land as a
// durable git commit rather than reaching done uncommitted. runFn performs the
// actual topos run, wired with the supplied non-blocking observer. The caller
// sets statusSet=true before invoking this.
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

	// Run the real commit pipeline so a native topos run produces a durable git
	// commit of the agent's worktree edits (stage -> commit -> rebase -> merge
	// into the default branch), matching the subprocess implement path's
	// auto-submit. Without this the run edits the worktree but reaches done with
	// the work uncommitted. r.Commit re-fetches the task, so it sees the worktree
	// paths execute.go persisted after this snapshot was taken.
	//
	// The commit is gated on a worktree being present. The native single-agent
	// path (runNativeTopos) runs in the task's worktree and commits here; the
	// multi-agent agentic path (runAgenticFlow) is dispatched before worktree
	// setup and runs in a topos temp sandbox with no worktree, so there is
	// nothing in the repo to commit and it walks straight to done as before.
	// Threading a worktree through the agentic path is deferred (tracked on #17).
	if cur, gErr := r.taskStore(taskID).GetTask(bgCtx, taskID); gErr == nil && cur != nil && len(cur.WorktreePaths) > 0 {
		if err := r.Commit(taskID, ""); err != nil {
			logger.Runner.Error("topos run commit", "task", taskID, "error", err)
			_ = r.taskStore(taskID).SetTaskFailureCategory(bgCtx, taskID, classifyFailure(err, false, ""))
			_ = r.taskStore(taskID).UpdateTaskStatus(bgCtx, taskID, store.TaskStatusFailed)
			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeError, map[string]string{"error": err.Error()})
			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeStateChange,
				store.NewStateChangeData(store.TaskStatusCommitting, store.TaskStatusFailed, store.TriggerSystem, nil))
			return
		}
	}

	_ = r.taskStore(taskID).UpdateTaskStatus(bgCtx, taskID, store.TaskStatusDone)
	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeStateChange,
		store.NewStateChangeData(store.TaskStatusCommitting, store.TaskStatusDone, store.TriggerSystem, nil))
}
