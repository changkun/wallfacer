package runner

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"changkun.de/x/wallfacer/internal/agents"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
)

// taskIDString returns the task's UUID as a string, or empty when the
// caller supplied a nil task (task-free invocations like the planning
// commit-message helper).
func taskIDString(task *store.Task) string {
	if task == nil {
		return ""
	}
	return task.ID.String()
}

// AgentRole aliases agents.Role so the runner stays decoupled from the
// descriptor definitions. New roles are declared in internal/agents and
// referenced from the runner via this alias.
type AgentRole = agents.Role

// runAgentOpts carries per-invocation parameters that don't belong on
// the role descriptor (they vary per call, not per role).
type runAgentOpts struct {
	// Labels are merged onto the container spec's labels. The
	// wallfacer.task.id and wallfacer.task.activity labels are
	// populated automatically; callers supply role-specific extras.
	Labels map[string]string
	// Context is an optional per-call context. When nil, runAgent uses
	// the runner's shutdown context with a role-derived timeout.
	Context context.Context
	// EmitSpanEvents controls whether runAgent records the
	// span_start / span_end task events around the launch. Callers that
	// own their own span accounting can disable this.
	EmitSpanEvents bool
	// TrackUsage controls whether runAgent calls AccumulateSubAgentUsage
	// + AppendTurnUsage on success. Task-free callers (planner commit,
	// health probes) set this to false.
	TrackUsage bool
	// Turn, when non-zero, is recorded as the TurnUsageRecord's turn
	// index. Defaults to 1 for single-turn roles.
	Turn int
	// ActivityOverride lets a caller attribute usage under a different
	// activity tag than the role's default. Oversight uses this to
	// split regular-run oversight and test-run oversight accounting
	// while sharing a single descriptor.
	ActivityOverride store.SandboxActivity
	// ContainerName, when non-empty, overrides the default
	// wallfacer-<role.Slug>-<uuid8> pattern. Refinement and ideation
	// use this for slugged / disambiguated names that predate the
	// migration. Prefer leaving it empty for new roles.
	ContainerName string
	// OnLaunch, when set, is invoked immediately after a successful
	// backend.Launch and before Stdout/Stderr are drained. Callers use
	// it to register the container handle in their per-role registries
	// (e.g. refineContainers) so log streams and kill commands can
	// attach to the in-flight container. Called for every launch
	// attempt, including the codex fallback.
	OnLaunch func(containerName string, handle sandbox.Handle)

	// SessionID, when non-empty, is threaded into buildAgentCmd as
	// --resume <id>. Used by the multi-turn heavyweight roles so the
	// second turn attaches to the first turn's conversation.
	SessionID string
	// ModelOverride, when non-empty, takes priority over
	// ModelResolver and the env-derived default. Heavyweight roles
	// use this for per-task model pinning.
	ModelOverride string
	// ModelResolver, when set, provides a per-call model lookup
	// used when ModelOverride is empty. Title and oversight use this
	// to route to the small-model env var without mutating the role
	// descriptor.
	ModelResolver func(sandbox.Type) string
	// WorktreeOverrides maps workspace host paths to the task's
	// worktree paths. mountReadWrite roles mount the worktrees in
	// place of the raw workspaces so commits land on the task's
	// branch.
	WorktreeOverrides map[string]string
	// BoardDir is the host directory containing board.json, mounted
	// read-only at /workspace/.tasks/ alongside any siblings. Only
	// honoured for mountReadWrite roles with MountBoard=true.
	BoardDir string
	// SiblingMounts maps shortID → (repoPath → worktreePath) for
	// read-only mounts of other in-progress task worktrees, so the
	// agent can reference sibling work without modifying it.
	SiblingMounts map[string]map[string]string
	// LiveLogWriter, when set, is tee'd alongside stdout and stderr
	// during the container run so callers can stream output while the
	// container is still alive. Heavyweight roles wire this to their
	// liveLogs registry.
	LiveLogWriter io.Writer
	// CircuitBreaker, when set, is consulted before every launch
	// attempt (Allow returning false short-circuits to an error) and
	// notified of failures (RecordFailure) and successes
	// (RecordSuccess). Only the heavyweight container-runtime CB is
	// currently wired.
	CircuitBreaker runAgentCircuitBreaker
}

// runAgentCircuitBreaker is the narrow surface runAgent needs from the
// container-runtime circuit breaker. Defined as an interface so tests
// and future backends can substitute a fake without importing the
// circuitbreaker package.
type runAgentCircuitBreaker interface {
	Allow() bool
	RecordFailure()
	RecordSuccess()
}

// agentResult is runAgent's return envelope. It bundles the parsed
// output, the role-specific structured result, and the raw streams so
// callers that need to persist turn output (heavyweight roles) can
// reach them without re-parsing.
type agentResult struct {
	Output      *agentOutput
	Parsed      any
	RawStdout   []byte
	RawStderr   []byte
	SandboxUsed sandbox.Type
}

// runAgent is the single launch primitive shared by every sub-agent
// role in the runner. For mountNone roles it handles: sandbox
// resolution, container spec construction, Launch, stdout/stderr
// drain, wait, NDJSON parsing, usage accumulation, and token-limit
// fallback. mountReadOnly and mountReadWrite support land in the
// sibling inspector-roles and heavyweight-roles tasks; calling
// runAgent with those modes today returns a "not yet implemented"
// error so a mis-wired caller fails loudly.
func (r *Runner) runAgent(
	ctx context.Context,
	role AgentRole,
	task *store.Task,
	prompt string,
	opts runAgentOpts,
) (*agentResult, error) {
	if role.Slug == "" {
		return nil, fmt.Errorf("runAgent: role.Slug is required")
	}
	binding, ok := bindingFor(role.Slug)
	if !ok {
		return nil, fmt.Errorf("runAgent: no binding registered for agent %q", role.Slug)
	}
	if binding.ParseResult == nil {
		return nil, fmt.Errorf("runAgent: binding for %s has no ParseResult", role.Slug)
	}

	// role.PromptTmpl carries a user-authored preamble that shapes
	// the agent's behaviour. When non-empty, prepend it to the
	// caller's prompt so the agent sees the preamble first and
	// the runtime input second, separated by a blank line. No
	// template substitution — the preamble is whatever the user
	// typed into the Agents-tab editor, verbatim. Built-in roles
	// leave PromptTmpl empty, so the default path is a no-op.
	//
	// Effective for agents invoked via Runner.RunAgent (the flow
	// engine's launcher). The turn-loop callers (GenerateTitle,
	// GenerateCommitMessage, GenerateOversight, RunRefinement,
	// RunIdeation) construct their own rendered prompts and
	// bypass this preamble; they're scoped to built-in roles with
	// empty PromptTmpl, so their behaviour is unchanged.
	if role.PromptTmpl != "" {
		prompt = role.PromptTmpl + "\n\n" + prompt
	}

	// Resolve the sandbox, newest tier first:
	//   1. role.Harness — the agent descriptor's explicit harness
	//      pin (set by user-authored clones). Wins over every
	//      per-task / env tier so a role marked "codex" always
	//      reaches Codex.
	//   2. Per-task per-activity override (SandboxByActivity).
	//   3. Per-task sandbox.
	//   4. Env-file per-activity setting.
	//   5. Env-file default sandbox.
	//   6. Claude (hardcoded fallback).
	primary := sandbox.Claude
	if pin := sandbox.Type(strings.ToLower(strings.TrimSpace(role.Harness))); pin.IsValid() {
		primary = pin
	} else if task != nil {
		primary = r.sandboxForTaskActivity(task, binding.Activity)
	}

	// Derive a per-call context with the role's timeout. Callers that
	// already own a sub-deadline supply their own context via opts.
	runCtx := ctx
	var cancel context.CancelFunc
	if runCtx == nil {
		runCtx = r.shutdownCtx
	}
	if binding.Timeout != nil {
		if d := binding.Timeout(task); d > 0 {
			runCtx, cancel = context.WithTimeout(runCtx, d)
			defer cancel()
		}
	}

	// Compose the container name once; the retry reuses it so users
	// watching logs see the original name rather than two unrelated
	// containers. Callers can provide an override (refinement uses a
	// slugged prompt in its name).
	containerName := opts.ContainerName
	if containerName == "" {
		containerName = "wallfacer-" + role.Slug + "-" + uuid.NewString()[:8]
	}

	// Activity used for sandbox resolution, label, span events, and
	// usage attribution. Callers may override via opts to split
	// sub-variants of the same role (oversight vs oversight-test).
	activity := binding.Activity
	if opts.ActivityOverride != "" {
		activity = opts.ActivityOverride
	}

	// Build labels. The activity label feeds the monitor UI.
	labels := map[string]string{
		"wallfacer.task.activity": string(activity),
	}
	if task != nil {
		labels["wallfacer.task.id"] = task.ID.String()
	}
	for k, v := range opts.Labels {
		labels[k] = v
	}

	// span_start / span_end bracket each launch attempt so the event
	// timeline shows a clean bar per container run — including retries.
	// Callers can suppress this via opts when they own their own
	// span accounting (the heavyweight turn loop will).
	launchOnce := func(sb sandbox.Type) (*agentResult, error) {
		if opts.EmitSpanEvents && task != nil {
			_ = r.taskStore(task.ID).InsertEvent(r.shutdownCtx, task.ID, store.EventTypeSpanStart,
				store.SpanData{Phase: "container_run", Label: string(activity)})
			defer func() {
				_ = r.taskStore(task.ID).InsertEvent(r.shutdownCtx, task.ID, store.EventTypeSpanEnd,
					store.SpanData{Phase: "container_run", Label: string(activity)})
			}()
		}
		return r.launchOne(runCtx, role, binding, containerName, prompt, sb, labels, task, opts)
	}

	result, err := launchOnce(primary)
	// Retry on token-limit-at-launch for Claude→Codex.
	if err != nil && primary == sandbox.Claude && isLikelyTokenLimitError(err.Error()) {
		logger.Runner.Warn("runAgent: claude token limit on launch; retrying with codex",
			"role", role.Slug, "container", containerName)
		if task != nil {
			r.recordFallbackEvent(task.ID, activity)
		}
		result, err = launchOnce(sandbox.Codex)
	}
	if err != nil {
		return nil, err
	}

	// Retry on token-limit-in-output for Claude→Codex. Agents sometimes
	// exit cleanly but signal a token-limit inside their result field
	// (e.g. --continue backoff), which the parser surfaces as IsError.
	if primary == sandbox.Claude && result.Output != nil && result.Output.IsError &&
		isLikelyTokenLimitError(result.Output.Result, result.Output.Subtype) {
		logger.Runner.Warn("runAgent: claude reported token limit in output; retrying with codex",
			"role", role.Slug, "container", containerName)
		if task != nil {
			r.recordFallbackEvent(task.ID, activity)
		}
		result, err = launchOnce(sandbox.Codex)
		if err != nil {
			return nil, err
		}
	}

	// Accumulate usage first so a downstream ParseResult failure still
	// counts the tokens/cost the agent consumed. The legacy per-role
	// call sites all billed before parsing and some tests depend on
	// that ordering (e.g. TestRunCostResumedFromWaiting exercises an
	// oversight failure path where the agent returned non-JSON output
	// but we still charge the invocation).
	if opts.TrackUsage && task != nil && result.Output != nil {
		r.accumulateAgentUsage(task.ID, activity, opts.Turn, result.Output)
	}

	// Parse the role-specific structured output.
	if result.Output != nil {
		parsed, parseErr := binding.ParseResult(result.Output)
		if parseErr != nil {
			return nil, fmt.Errorf("%s: %w", role.Slug, parseErr)
		}
		result.Parsed = parsed
	}

	return result, nil
}

// launchOne builds the container spec, invokes backend.Launch, drains
// the streams, and parses the NDJSON result for a single sub-agent
// invocation. It dispatches on binding.MountMode to add the right volume
// mounts and feeds optional heavyweight concerns (live-log tee,
// circuit breaker) when supplied via opts.
func (r *Runner) launchOne(
	ctx context.Context,
	role AgentRole,
	binding agentBinding,
	containerName, prompt string,
	sb sandbox.Type,
	labels map[string]string,
	task *store.Task,
	opts runAgentOpts,
) (*agentResult, error) {
	// Short-circuit on an open container-runtime circuit breaker. Only
	// heavyweight callers wire this today; the header roles pass nil.
	if opts.CircuitBreaker != nil && !opts.CircuitBreaker.Allow() {
		return nil, fmt.Errorf("%s: container circuit breaker open: container runtime may be unavailable", role.Slug)
	}

	// Resolve the model with the explicit override > role-specific
	// resolver > env-derived default. This mirrors the priority
	// buildContainerSpecForSandbox applied before the migration.
	model := opts.ModelOverride
	if model == "" {
		if opts.ModelResolver != nil {
			model = opts.ModelResolver(sb)
		}
		if model == "" && binding.Model != nil {
			model = binding.Model(sb)
		}
		if model == "" {
			model = r.modelFromEnvForSandbox(sb)
		}
	}

	var spec sandbox.ContainerSpec
	switch binding.MountMode {
	case mountReadWrite:
		// Heavyweight spec builder already handles worktree mounts,
		// board + sibling context, and per-task labels.
		spec = r.buildContainerSpecForSandbox(
			containerName,
			taskIDString(task),
			prompt,
			opts.SessionID,
			opts.WorktreeOverrides,
			opts.BoardDir,
			opts.SiblingMounts,
			opts.ModelOverride,
			sb,
		)
	default:
		spec = r.buildInspectorSpec(containerName, model, sb, binding.MountMode)
		spec.Cmd = buildAgentCmd(prompt, model)
		if opts.SessionID != "" {
			spec.Cmd = append(spec.Cmd, "--resume", opts.SessionID)
		}
	}

	// Clone the labels map so a caller that hands us a shared map (the
	// migrated title/oversight/commit call sites do) cannot be mutated
	// by the backend or by a later retry.
	merged := make(map[string]string, len(spec.Labels)+len(labels))
	for k, v := range spec.Labels {
		merged[k] = v
	}
	for k, v := range labels {
		merged[k] = v
	}
	spec.Labels = merged

	handle, launchErr := r.backend.Launch(ctx, spec)
	if launchErr != nil {
		if opts.CircuitBreaker != nil {
			opts.CircuitBreaker.RecordFailure()
		}
		return nil, fmt.Errorf("launch %s container: %w", role.Slug, launchErr)
	}
	if opts.OnLaunch != nil {
		opts.OnLaunch(containerName, handle)
	}

	// Stdout / stderr are tee'd into the optional live-log writer so
	// callers can stream output while the container is still alive.
	// When no writer is supplied we fall back to a direct ReadAll.
	var rawStdout, rawStderr []byte
	if opts.LiveLogWriter != nil {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			rawStdout, _ = io.ReadAll(io.TeeReader(handle.Stdout(), opts.LiveLogWriter))
		}()
		go func() {
			defer wg.Done()
			rawStderr, _ = io.ReadAll(io.TeeReader(handle.Stderr(), opts.LiveLogWriter))
		}()
		wg.Wait()
	} else {
		rawStdout, _ = io.ReadAll(handle.Stdout())
		rawStderr, _ = io.ReadAll(handle.Stderr())
	}
	exitCode, waitErr := handle.Wait()

	// Exit code 125 is the container runtime's "engine error" signal
	// (podman / docker). Record it against the circuit breaker even
	// when ctx is still alive so repeated engine failures trip it.
	if exitCode == 125 && ctx.Err() == nil && opts.CircuitBreaker != nil {
		opts.CircuitBreaker.RecordFailure()
	}

	// Context cancellation → report as terminated (matches the legacy
	// title/oversight/commit behaviour).
	if ctx.Err() != nil {
		_ = handle.Kill()
		return nil, fmt.Errorf("%s container terminated: %w", role.Slug, ctx.Err())
	}

	raw := strings.TrimSpace(string(rawStdout))
	if raw == "" {
		// Surface the wait error when present — classifyFailure keys
		// on "exit status" or "empty output" to bucket container
		// crashes into the retry-eligible category, so keep those
		// phrases in the message verbatim.
		if waitErr != nil {
			// Wait error carries "exit status N" which classifyFailure
			// keys on to bucket container crashes.
			return nil, fmt.Errorf("%s: exec container: %w", role.Slug, waitErr)
		}
		if exitCode != 0 {
			// No wait error: distinguish this from a crash so the
			// auto-retry classifier treats it as Unknown (no retry).
			// Matches pre-migration behaviour in the heavyweight
			// runContainer path.
			return nil, fmt.Errorf("%s container exited with code %d: stderr=%s",
				role.Slug, exitCode, truncate(string(rawStderr), 200))
		}
		return nil, fmt.Errorf("%s: empty output", role.Slug)
	}

	// Parse first so a non-zero exit with a valid final NDJSON payload
	// still counts as success — several existing tests cover this
	// tolerant behaviour for title + oversight, and the legacy code
	// paths all implemented it.
	output, err := parseOutput(raw)
	if err != nil {
		if exitCode != 0 {
			return nil, fmt.Errorf("%s container exited with code %d: stderr=%s stdout=%s",
				role.Slug, exitCode, truncate(string(rawStderr), 200), truncate(raw, 200))
		}
		// Wording matches the pre-migration message so callers that
		// grep on "parse output" (some tests do) still match.
		return nil, fmt.Errorf("%s: parse output: %w (raw: %s)", role.Slug, err, truncate(raw, 200))
	}
	if exitCode != 0 {
		logger.Runner.Warn(role.Slug+": container exited non-zero but produced valid output",
			"code", exitCode, "sandbox", sb, "model", model)
	}
	// Successful happy-path → mark the circuit breaker healthy if one
	// is configured. Opening-then-closing tracks runtime state when a
	// transient engine issue resolved itself.
	if opts.CircuitBreaker != nil {
		opts.CircuitBreaker.RecordSuccess()
	}
	output.ActualSandbox = sb
	return &agentResult{
		Output:      output,
		RawStdout:   rawStdout,
		RawStderr:   rawStderr,
		SandboxUsed: sb,
	}, nil
}

// buildInspectorSpec produces a ContainerSpec for headless or read-only
// inspector roles. For mountNone it returns the same spec
// buildBaseContainerSpec produces. For mountReadOnly it layers every
// configured workspace as a read-only volume plus the workspace
// instructions file (CLAUDE.md / AGENTS.md) and sets WorkDir so the
// agent has a natural CWD to inspect from. Worker-container mode is
// intentionally not involved — inspector roles are short-lived and
// cheap to spin up ephemerally.
func (r *Runner) buildInspectorSpec(
	containerName, model string,
	sb sandbox.Type,
	mode mountMode,
) sandbox.ContainerSpec {
	spec := r.buildBaseContainerSpec(containerName, model, sb)
	if mode != mountReadOnly {
		return spec
	}

	workspaces := r.currentWorkspaces()
	var basenames []string
	for _, ws := range workspaces {
		ws = strings.TrimSpace(ws)
		if ws == "" {
			continue
		}
		basename := sanitizeBasename(ws)
		basenames = append(basenames, basename)
		spec.Volumes = append(spec.Volumes, sandbox.VolumeMount{
			Host:      ws,
			Container: "/workspace/" + basename,
			Options:   mountOpts("z", "ro"),
		})
	}
	spec.Volumes = r.appendInstructionsMount(spec.Volumes, sb, basenames)

	workdir := "/workspace"
	if len(basenames) == 1 {
		workdir = "/workspace/" + basenames[0]
	}
	spec.WorkDir = workdir
	return spec
}

// recordFallbackEvent writes a system event noting that the runner fell
// back from Claude to Codex for this activity. Mirrors the inline
// messages the role-specific code paths emit today so operators see
// the same signal in the event trail after migration.
func (r *Runner) recordFallbackEvent(taskID uuid.UUID, activity store.SandboxActivity) {
	_ = r.taskStore(taskID).InsertEvent(r.shutdownCtx, taskID, store.EventTypeSystem, map[string]string{
		"result": fmt.Sprintf("Sandbox fallback: claude → codex (token/rate limit during %s)", activity),
	})
}

// accumulateAgentUsage bumps the per-sub-agent usage totals on the task
// and appends a turn record so the UI's cost dashboard and the per-
// turn drill-down both see the invocation.
func (r *Runner) accumulateAgentUsage(
	taskID uuid.UUID,
	activity store.SandboxActivity,
	turn int,
	output *agentOutput,
) {
	if output.Usage.InputTokens == 0 && output.Usage.OutputTokens == 0 && output.TotalCostUSD == 0 {
		return
	}
	if turn == 0 {
		turn = 1
	}
	_ = r.taskStore(taskID).AccumulateSubAgentUsage(r.shutdownCtx, taskID, activity, store.TaskUsage{
		InputTokens:          output.Usage.InputTokens,
		OutputTokens:         output.Usage.OutputTokens,
		CacheReadInputTokens: output.Usage.CacheReadInputTokens,
		CacheCreationTokens:  output.Usage.CacheCreationInputTokens,
		CostUSD:              output.TotalCostUSD,
	})
	if err := r.taskStore(taskID).AppendTurnUsage(taskID, store.TurnUsageRecord{
		Turn:                 turn,
		Timestamp:            time.Now().UTC(),
		InputTokens:          output.Usage.InputTokens,
		OutputTokens:         output.Usage.OutputTokens,
		CacheReadInputTokens: output.Usage.CacheReadInputTokens,
		CacheCreationTokens:  output.Usage.CacheCreationInputTokens,
		CostUSD:              output.TotalCostUSD,
		Sandbox:              output.ActualSandbox,
		SubAgent:             activity,
	}); err != nil {
		logger.Runner.Warn("runAgent: append turn usage failed",
			"task", taskID, "activity", activity, "error", err)
	}
}
