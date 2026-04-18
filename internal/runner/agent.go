package runner

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
)

// MountMode enumerates the three fundamental container-mount profiles the
// sub-agent roles in this package fall into. The parent spec's audit
// (specs/shared/agent-abstraction.md) grouped the seven existing roles
// into these exact tiers.
type MountMode int

const (
	// MountNone gives the container no workspace access. Suits headless
	// roles — title, oversight, commit-message — whose input is the
	// task's prompt and (for oversight/commit) a bundle of pre-rendered
	// text. No read or write to the host filesystem beyond the image.
	MountNone MountMode = iota
	// MountReadOnly mounts every configured workspace read-only plus the
	// workspace instructions file. Suits inspector roles — refinement,
	// ephemeral ideation — that need to read the code but must not
	// modify it.
	MountReadOnly
	// MountReadWrite mounts each task worktree read-write and, when
	// AgentRole.MountBoard is true, the board manifest + sibling
	// worktrees read-only. Suits heavyweight roles — implementation
	// and testing — that produce commits.
	MountReadWrite
)

// AgentRole is a declarative descriptor for one kind of sub-agent. The
// runner's runAgent function dispatches on the descriptor's fields
// instead of calling role-specific launcher code, so adding a new role
// reduces to defining a new AgentRole value + (when needed) a template
// and a ParseResult.
type AgentRole struct {
	// Activity names the per-activity sandbox routing bucket (feeds
	// sandboxForTaskActivity). Required.
	Activity store.SandboxActivity
	// Name is the kebab-case identifier used when composing container
	// names: wallfacer-<Name>-<uuid8>. Required.
	Name string
	// Timeout is a function so roles whose timeout depends on the task
	// (implementation, idea-agent) can derive it at call time. Roles
	// with a fixed timeout return a constant.
	Timeout func(*store.Task) time.Duration
	// MountMode selects the workspace-mount profile. See the MountMode
	// constants for the semantics of each tier.
	MountMode MountMode
	// MountBoard, when true, mounts the board manifest and sibling
	// worktrees read-only alongside the workspace. Only meaningful for
	// MountReadWrite roles today.
	MountBoard bool
	// SingleTurn, when true, skips the --resume session loop. Headless
	// and inspector roles use SingleTurn=true; the heavyweight turn
	// loop in execute.go drives multi-turn roles itself.
	SingleTurn bool
	// ParseResult extracts the role-specific structured output from the
	// raw `agentOutput.Result` string. Returning any lets each role
	// decode its own schema without leaking a shared type. The concrete
	// type is documented in the role's descriptor comment.
	ParseResult func(output *agentOutput) (any, error)
	// Model, when non-nil, overrides the default per-sandbox model
	// lookup for this role. Title uses CLAUDE_TITLE_MODEL; other
	// roles inherit CLAUDE_DEFAULT_MODEL via r.modelFromEnvForSandbox.
	// Nil means "use the runner's default model resolver".
	Model func(sb sandbox.Type) string
}

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
	// wallfacer-<role.Name>-<uuid8> pattern. Refinement and ideation
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
// role in the runner. For MountNone roles it handles: sandbox
// resolution, container spec construction, Launch, stdout/stderr
// drain, wait, NDJSON parsing, usage accumulation, and token-limit
// fallback. MountReadOnly and MountReadWrite support land in the
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
	if role.MountMode == MountReadWrite {
		return nil, fmt.Errorf("runAgent: mount mode %v not yet implemented", role.MountMode)
	}
	if role.Name == "" {
		return nil, fmt.Errorf("runAgent: role.Name is required")
	}
	if role.ParseResult == nil {
		return nil, fmt.Errorf("runAgent: role.ParseResult is required for %s", role.Name)
	}

	// Resolve the sandbox for the task+activity using the existing 4-tier
	// resolver (per-task activity override → per-task sandbox → env file
	// → Claude). Task-free callers pass task=nil and get Claude.
	primary := sandbox.Claude
	if task != nil {
		primary = r.sandboxForTaskActivity(task, role.Activity)
	}

	// Derive a per-call context with the role's timeout. Callers that
	// already own a sub-deadline supply their own context via opts.
	runCtx := ctx
	var cancel context.CancelFunc
	if runCtx == nil {
		runCtx = r.shutdownCtx
	}
	if role.Timeout != nil {
		if d := role.Timeout(task); d > 0 {
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
		containerName = "wallfacer-" + role.Name + "-" + uuid.NewString()[:8]
	}

	// Activity used for sandbox resolution, label, span events, and
	// usage attribution. Callers may override via opts to split
	// sub-variants of the same role (oversight vs oversight-test).
	activity := role.Activity
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
		return r.launchOne(runCtx, role, containerName, prompt, sb, labels, opts.OnLaunch)
	}

	result, err := launchOnce(primary)
	// Retry on token-limit-at-launch for Claude→Codex.
	if err != nil && primary == sandbox.Claude && isLikelyTokenLimitError(err.Error()) {
		logger.Runner.Warn("runAgent: claude token limit on launch; retrying with codex",
			"role", role.Name, "container", containerName)
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
			"role", role.Name, "container", containerName)
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
		parsed, parseErr := role.ParseResult(result.Output)
		if parseErr != nil {
			return nil, fmt.Errorf("%s: %w", role.Name, parseErr)
		}
		result.Parsed = parsed
	}

	return result, nil
}

// launchOne builds the container spec, invokes backend.Launch, drains
// the streams, and parses the NDJSON result for a single sub-agent
// invocation. It dispatches on role.MountMode to add the right volume
// mounts: MountNone is a minimal headless launch, MountReadOnly layers
// read-only workspace + instructions mounts on top so the agent can
// inspect the code without modifying it.
func (r *Runner) launchOne(
	ctx context.Context,
	role AgentRole,
	containerName, prompt string,
	sb sandbox.Type,
	labels map[string]string,
	onLaunch func(string, sandbox.Handle),
) (*agentResult, error) {
	model := r.modelFromEnvForSandbox(sb)
	if role.Model != nil {
		if m := role.Model(sb); m != "" {
			model = m
		}
	}
	spec := r.buildInspectorSpec(containerName, model, sb, role.MountMode)
	// Clone the labels map so a caller that hands us a shared map (the
	// migrated title/oversight/commit call sites do) cannot be mutated
	// by the backend or by a later retry.
	spec.Labels = make(map[string]string, len(labels))
	for k, v := range labels {
		spec.Labels[k] = v
	}
	spec.Cmd = buildAgentCmd(prompt, model)

	handle, launchErr := r.backend.Launch(ctx, spec)
	if launchErr != nil {
		return nil, fmt.Errorf("launch %s container: %w", role.Name, launchErr)
	}
	if onLaunch != nil {
		onLaunch(containerName, handle)
	}
	rawStdout, _ := io.ReadAll(handle.Stdout())
	rawStderr, _ := io.ReadAll(handle.Stderr())
	exitCode, _ := handle.Wait()

	// Context cancellation → report as terminated (matches the legacy
	// title/oversight/commit behaviour).
	if ctx.Err() != nil {
		_ = handle.Kill()
		return nil, fmt.Errorf("%s container terminated: %w", role.Name, ctx.Err())
	}

	raw := strings.TrimSpace(string(rawStdout))
	if raw == "" {
		if exitCode != 0 {
			return nil, fmt.Errorf("%s container exited with code %d: stderr=%s",
				role.Name, exitCode, truncate(string(rawStderr), 200))
		}
		return nil, fmt.Errorf("%s: empty output", role.Name)
	}

	// Parse first so a non-zero exit with a valid final NDJSON payload
	// still counts as success — several existing tests cover this
	// tolerant behaviour for title + oversight, and the legacy code
	// paths all implemented it.
	output, err := parseOutput(raw)
	if err != nil {
		if exitCode != 0 {
			return nil, fmt.Errorf("%s container exited with code %d: stderr=%s stdout=%s",
				role.Name, exitCode, truncate(string(rawStderr), 200), truncate(raw, 200))
		}
		return nil, fmt.Errorf("%s parse failure: raw=%s", role.Name, truncate(raw, 200))
	}
	if exitCode != 0 {
		logger.Runner.Warn(role.Name+": container exited non-zero but produced valid output",
			"code", exitCode, "sandbox", sb, "model", model)
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
// inspector roles. For MountNone it returns the same spec
// buildBaseContainerSpec produces. For MountReadOnly it layers every
// configured workspace as a read-only volume plus the workspace
// instructions file (CLAUDE.md / AGENTS.md) and sets WorkDir so the
// agent has a natural CWD to inspect from. Worker-container mode is
// intentionally not involved — inspector roles are short-lived and
// cheap to spin up ephemerally.
func (r *Runner) buildInspectorSpec(
	containerName, model string,
	sb sandbox.Type,
	mode MountMode,
) sandbox.ContainerSpec {
	spec := r.buildBaseContainerSpec(containerName, model, sb)
	if mode != MountReadOnly {
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
