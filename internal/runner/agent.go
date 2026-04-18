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
}

// agentResult is runAgent's return envelope. It bundles the parsed
// output, the role-specific structured result, and the raw streams so
// callers that need to persist turn output (heavyweight roles) can
// reach them without re-parsing.
type agentResult struct {
	Output     *agentOutput
	Parsed     any
	RawStdout  []byte
	RawStderr  []byte
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
	if role.MountMode != MountNone {
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
	// containers.
	containerName := "wallfacer-" + role.Name + "-" + uuid.NewString()[:8]

	// Build labels. The activity label feeds the monitor UI.
	labels := map[string]string{
		"wallfacer.task.activity": string(role.Activity),
	}
	if task != nil {
		labels["wallfacer.task.id"] = task.ID.String()
	}
	for k, v := range opts.Labels {
		labels[k] = v
	}

	result, err := r.launchHeadless(runCtx, role, containerName, prompt, primary, labels)
	// Retry on token-limit-at-launch for Claude→Codex.
	if err != nil && primary == sandbox.Claude && isLikelyTokenLimitError(err.Error()) {
		logger.Runner.Warn("runAgent: claude token limit on launch; retrying with codex",
			"role", role.Name, "container", containerName)
		if task != nil {
			r.recordFallbackEvent(task.ID, role.Activity)
		}
		result, err = r.launchHeadless(runCtx, role, containerName, prompt, sandbox.Codex, labels)
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
			r.recordFallbackEvent(task.ID, role.Activity)
		}
		result, err = r.launchHeadless(runCtx, role, containerName, prompt, sandbox.Codex, labels)
		if err != nil {
			return nil, err
		}
	}

	// Parse the role-specific structured output.
	if result.Output != nil {
		parsed, parseErr := role.ParseResult(result.Output)
		if parseErr != nil {
			return nil, fmt.Errorf("%s: %w", role.Name, parseErr)
		}
		result.Parsed = parsed
	}

	// Accumulate usage and append a turn record for task-bound calls.
	if opts.TrackUsage && task != nil && result.Output != nil {
		r.accumulateAgentUsage(task.ID, role.Activity, opts.Turn, result.Output)
	}

	return result, nil
}

// launchHeadless builds the container spec and runs the launch for a
// MountNone role. Kept as a separate function so the retry path reuses
// it without duplicating the spec construction.
func (r *Runner) launchHeadless(
	ctx context.Context,
	role AgentRole,
	containerName, prompt string,
	sb sandbox.Type,
	labels map[string]string,
) (*agentResult, error) {
	model := r.modelFromEnvForSandbox(sb)
	spec := r.buildBaseContainerSpec(containerName, model, sb)
	spec.Labels = labels
	spec.Cmd = buildAgentCmd(prompt, model)

	handle, launchErr := r.backend.Launch(ctx, spec)
	if launchErr != nil {
		return nil, fmt.Errorf("launch %s container: %w", role.Name, launchErr)
	}
	rawStdout, _ := io.ReadAll(handle.Stdout())
	rawStderr, _ := io.ReadAll(handle.Stderr())
	exitCode, _ := handle.Wait()

	if exitCode != 0 && ctx.Err() == nil {
		return nil, fmt.Errorf("%s container exited with code %d: stderr=%s",
			role.Name, exitCode, truncate(string(rawStderr), 200))
	}

	raw := strings.TrimSpace(string(rawStdout))
	if raw == "" {
		return nil, fmt.Errorf("%s: empty output", role.Name)
	}

	output, err := parseOutput(raw)
	if err != nil {
		return nil, fmt.Errorf("%s parse failure: raw=%s", role.Name, truncate(raw, 200))
	}
	output.ActualSandbox = sb
	return &agentResult{
		Output:      output,
		RawStdout:   rawStdout,
		RawStderr:   rawStderr,
		SandboxUsed: sb,
	}, nil
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
