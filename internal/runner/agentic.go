package runner

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/agentgraph"
	"latere.ai/x/wallfacer/internal/constants"
	"latere.ai/x/wallfacer/internal/envconfig"
	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/store"
)

// agenticModelConfig derives the model selection for an in-process run from
// wallfacer's global credential settings (the .env file), the same source the
// subprocess harnesses read. A configured ANTHROPIC_BASE_URL routes through Lux
// (the gateway); a bare key talks to the provider directly. Only the static
// x-api-key credential is wired for now; Bearer-style tokens
// (ANTHROPIC_AUTH_TOKEN, CLAUDE_CODE_OAUTH_TOKEN) are deferred (they need a
// per-call BearerSource).
//
// Without an ANTHROPIC_API_KEY (no env file, an unreadable one, or one that
// does not set the key) it returns agentgraph.ErrNoModelCredential, and a
// secret-store failure returns envconfig.ErrSecretStore: either way the caller
// has no config to run with. The one exception is a Runner whose allowTestModel
// is set, which gets the deterministic test model instead of the refusal.
func (r *Runner) agenticModelConfig() (agentgraph.ModelConfig, error) {
	var cfg envconfig.Config
	if r.envFile != "" {
		parsed, err := envconfig.Parse(r.envFile)
		if errors.Is(err, envconfig.ErrSecretStore) {
			return agentgraph.ModelConfig{}, err
		}
		// Parse returns an empty Config on ordinary file errors, which reads
		// as no credential below.
		cfg = parsed
	}
	if cfg.APIKey == "" {
		if r.allowTestModel {
			return agentgraph.ModelConfig{Mode: agentgraph.ModelModeFake}, nil
		}
		return agentgraph.ModelConfig{}, agentgraph.ErrNoModelCredential
	}
	mode := agentgraph.ModelModeDirect
	baseURL := ""
	if cfg.BaseURL != "" {
		mode = agentgraph.ModelModeLux
		baseURL = gatewayRoot(cfg.BaseURL)
	}
	return agentgraph.ModelConfig{
		Mode:     mode,
		Provider: "anthropic",
		Model:    cfg.DefaultModel,
		BaseURL:  baseURL,
		APIKey:   cfg.APIKey,
	}, nil
}

// toposFailure maps the error of an in-process run onto the task's failure
// record: the category, the text stored as the task result, and the developer
// detail kept beside it on the error event. A missing model credential has its
// own category and the one fixed sentence, with the underlying error as detail;
// any other error is classified as usual and carries its own text.
func toposFailure(err error) (category store.FailureCategory, message, detail string) {
	if errors.Is(err, agentgraph.ErrNoModelCredential) {
		return store.FailureCategoryModelCredential, harness.ToposCredentialRequired, err.Error()
	}
	return classifyFailure(err, false, ""), err.Error(), ""
}

// failToposRun moves an in-progress task to failed for an in-process run that
// did not start or did not finish, recording the category, result text, and
// error event toposFailure derives from err. It applies the auto-retry budget
// first and leaves the task in backlog when a retry was scheduled.
func (r *Runner) failToposRun(bgCtx context.Context, taskID uuid.UUID, err error) {
	logger.Runner.Error("topos run", "task", taskID, "error", err)
	category, message, detail := toposFailure(err)
	_ = r.taskStore(taskID).SetTaskFailureCategory(bgCtx, taskID, category)
	if r.tryAutoRetry(bgCtx, taskID, category) {
		return
	}
	event := map[string]string{"error": message}
	if detail != "" {
		event["detail"] = detail
	}
	_ = r.taskStore(taskID).UpdateTaskStatus(bgCtx, taskID, store.TaskStatusFailed)
	_ = r.taskStore(taskID).UpdateTaskResult(bgCtx, taskID, message, "", "", 0)
	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeError, event)
	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeStateChange,
		store.NewStateChangeData(store.TaskStatusInProgress, store.TaskStatusFailed, store.TriggerSystem, nil))
}

// gatewayRoot reduces the .env's ANTHROPIC_BASE_URL, which is shaped for the
// container harness (Claude Code dials the gateway's anthropic-wire door, e.g.
// https://api.latere.ai/v1/models/anthropic), to the gateway root the
// lux-native dialect (POST /lux/v1/generate) lives under, e.g.
// https://api.latere.ai/v1/models. Only the trailing /anthropic door segment
// is dropped: a gateway served under a base path keeps that path, and one at
// the root of its host reduces to the origin. Query and fragment are dropped.
// The .env stays harness-shaped; the model leg derives the base it needs. An
// unparseable value passes through untouched so the resulting request error
// names the configured URL.
func gatewayRoot(harnessBase string) string {
	u, err := url.Parse(harnessBase)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return harnessBase
	}
	path := strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/anthropic")
	return u.Scheme + "://" + u.Host + path
}

// agenticEvent maps a topos trace event to a task-timeline event, returning
// ok=false for events that should not surface (lifecycle bookkeeping, empty
// payloads). It renders an in-process run as a readable live trace: each agent
// turn's assistant text, any delegation, and tool use. The agent label is the
// trace node id (ev.Node) so the timeline lines join to graph nodes.
func agenticEvent(ev agentgraph.Event) (store.EventType, map[string]string, bool) {
	label := ev.AgentID
	if label == "" {
		label = ev.Node
	}
	// trace builds the timeline data: a human "result" line (so the events tab
	// reads naturally) plus structured fields the Agent Graph view groups on
	// (source marks it as an agent-graph trace; node is the trace join key).
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

// runNativeTopos executes a task through the native Topos harness: a single
// in-process agent (a one-node topos region). It is the path a task resolves
// to when its harness is Topos (the native default, once harness.Default() is
// flipped; until then only an explicit topos pin reaches here). It produces a
// final text + trace and walks the state machine; driveToposRun then runs the
// real commit pipeline so the worktree edits land as a durable git commit.
// Verification (the test step) parity with the subprocess harnesses is
// tracked in the topos-native-harness spec. The caller sets statusSet=true
// before invoking this.
func (r *Runner) runNativeTopos(bgCtx context.Context, taskID uuid.UUID, task store.Task, prompt, worktree string) {
	// worktree is the task's set-up worktree (the real repo) so the agent's tools
	// edit actual files; an empty worktree falls back to the topos temp-dir sandbox.
	r.driveToposRun(bgCtx, taskID, task, func(ctx context.Context, onEvent func(agentgraph.Event)) (agentgraph.Result, error) {
		cfg, err := r.agenticModelConfig()
		if err != nil {
			return agentgraph.Result{}, err
		}
		return agentgraph.RunAgent(ctx, task.ID.String(), cfg, "implement", "", prompt, worktree, onEvent)
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

// driveToposRun executes an in-process topos run supplied as runFn and maps the
// outcome onto the task. It forwards the run's live trace events onto the task
// timeline (so the per-turn assistant text and tool use are visible as the run
// proceeds, not just as a trace graph at the end), persists the final text and
// the JSON-marshaled trace graph, then walks the task through
// in_progress -> waiting -> committing -> done (the state machine forbids a
// direct in_progress -> done transition). In the committing phase it runs the
// real commit pipeline (r.Commit) when the run has a worktree, so the run's
// edits land as a durable git commit rather than reaching done uncommitted.
// runFn performs the actual topos run, wired with the supplied non-blocking
// observer. The caller sets statusSet=true before invoking this.
func (r *Runner) driveToposRun(bgCtx context.Context, taskID uuid.UUID, task store.Task, runFn func(ctx context.Context, onEvent func(agentgraph.Event)) (agentgraph.Result, error)) {
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
	traceCh := make(chan agentgraph.Event, 256)
	traceDone := make(chan struct{})
	go func() {
		defer close(traceDone)
		for ev := range traceCh {
			etype, data, ok := agenticEvent(ev)
			if !ok {
				continue
			}
			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, etype, data)
		}
	}()
	onEvent := func(ev agentgraph.Event) {
		select {
		case traceCh <- ev:
		default: // buffer full: drop rather than stall the run
		}
	}

	res, err := runFn(ctx, onEvent)
	close(traceCh)
	<-traceDone
	if captureErr := r.captureTaskCommits(bgCtx, taskID, task.Turns+1, nil); captureErr != nil {
		r.failCommitHistory(bgCtx, taskID, captureErr)
		return
	}
	if err != nil {
		if cur, _ := r.taskStore(taskID).GetTask(bgCtx, taskID); cur != nil && cur.Status == store.TaskStatusCancelled {
			return
		}
		r.failToposRun(bgCtx, taskID, err)
		return
	}

	// Persist the result and trace before transitioning so the durable record
	// is complete the moment the task reaches done.
	_ = r.taskStore(taskID).UpdateTaskResult(bgCtx, taskID, res.Final, "", "end_turn", 0)
	if data, mErr := json.Marshal(res.Trace); mErr == nil {
		if lErr := r.taskStore(taskID).UpdateTaskTrace(bgCtx, taskID, string(data)); lErr != nil {
			logger.Runner.Warn("topos run trace persist", "task", taskID, "error", lErr)
		}
	} else {
		logger.Runner.Warn("topos run trace marshal", "task", taskID, "error", mErr)
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
	// The commit is gated on a worktree being present. A native run executes in
	// the task worktree and commits here; tests without a configured workspace
	// keep using a temporary sandbox.
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
