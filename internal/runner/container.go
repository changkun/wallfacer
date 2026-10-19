package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"latere.ai/x/wallfacer/internal/workspace"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/agents"
	"latere.ai/x/wallfacer/internal/envconfig"
	"latere.ai/x/wallfacer/internal/executor"
	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/pkg/livelog"
	"latere.ai/x/wallfacer/internal/store"
)

// agentUsage mirrors the token-usage JSON object emitted by the
// agent container.
type agentUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// agentOutput is the top-level result object emitted by an agent
// container. ActualSandbox is populated by the runner, not parsed.
type agentOutput struct {
	Result        string     `json:"result"`
	SessionID     string     `json:"session_id"`
	ThreadID      string     `json:"thread_id,omitempty"`
	StopReason    string     `json:"stop_reason"`
	Subtype       string     `json:"subtype"`
	IsError       bool       `json:"is_error"`
	TotalCostUSD  float64    `json:"total_cost_usd"`
	Usage         agentUsage `json:"usage"`
	ActualSandbox harness.ID `json:"-"`
	// ObservedModel is the model the harness reported during the run (from
	// the init/assistant events), not the model the runner requested. Empty
	// when the harness does not report one. Populated by parseHarnessOutput.
	ObservedModel string `json:"-"`
}

// Package-level aliases for SandboxActivity constants to reduce verbosity
// in sandbox routing call sites throughout the runner package.
const (
	activityImplementation = store.SandboxActivityImplementation
	activityOversight      = store.SandboxActivityOversight
	activityCommitMessage  = store.SandboxActivityCommitMessage
)

// buildContainerSpecForSandbox constructs a ContainerSpec for the given sandbox
// with all workspace mounts, labels, board context, and agent command configured.
func (r *Runner) buildContainerSpecForSandbox(
	containerName, taskID, prompt, sessionID string,
	worktreeOverrides map[string]string,
	boardDir string,
	siblingMounts map[string]map[string]string,
	modelOverride string,
	sb harness.ID,
) executor.ContainerSpec {
	// Resolve model once: override takes priority, then env default.
	model := modelOverride
	if model == "" {
		model = r.modelFromEnvForSandbox(sb)
	}

	spec := r.buildBaseContainerSpec(containerName, sb)

	// Label the task so the monitor can correlate the host process to a task.
	if taskID != "" {
		spec.Labels = map[string]string{
			"wallfacer.task.id":     taskID,
			"wallfacer.task.prompt": truncate(prompt, 80),
		}
	}

	return r.buildHostSpec(spec, prompt, model, sessionID, sb, worktreeOverrides, boardDir, siblingMounts)
}

// buildHostSpec fills in the fields of a base launch spec for host-mode
// execution. The composed workspace instructions, the board manifest, and
// the sibling-worktree table are surfaced to the agent via WALLFACER_*
// environment variables the HostBackend and agents can consult.
//
// CWD:
//   - Single workspace: CWD is the host path of that workspace (worktree
//     override applied when available).
//   - Multi-workspace: pick the first; the agent can reach the others via
//     the manifest file referenced by WALLFACER_SIBLING_WORKTREES_JSON.
func (r *Runner) buildHostSpec(
	spec executor.ContainerSpec,
	prompt, model, sessionID string,
	_ harness.ID,
	worktreeOverrides map[string]string,
	boardDir string,
	siblingMounts map[string]map[string]string,
) executor.ContainerSpec {
	// Resolve CWD from the workspace list, preferring the worktree override.
	workspaces := r.currentWorkspaces()
	workDir := ""
	for _, ws := range workspaces {
		ws = strings.TrimSpace(ws)
		if ws == "" {
			continue
		}
		if wt, ok := worktreeOverrides[ws]; ok {
			workDir = wt
		} else {
			workDir = ws
		}
		break
	}
	spec.WorkDir = workDir

	// Surface board / siblings via env vars.
	if boardDir != "" {
		boardPath := filepath.Join(boardDir, "board.json")
		if _, err := os.Stat(boardPath); err == nil {
			spec.Env["WALLFACER_BOARD_JSON"] = boardPath
		}
		if manifestPath, err := writeSiblingManifest(boardDir, siblingMounts); err == nil && manifestPath != "" {
			spec.Env["WALLFACER_SIBLING_WORKTREES_JSON"] = manifestPath
		} else if err != nil {
			logger.Runner.Warn("host mode: write sibling manifest", "error", err)
		}
	}

	spec.Cmd = buildAgentCmd(prompt, model)
	if sessionID != "" {
		spec.Cmd = append(spec.Cmd, "--resume", sessionID)
	}
	return spec
}

// writeSiblingManifest serializes the siblingMounts map to
// boardDir/sibling_worktrees.json. Returns the absolute path and nil on
// success, or "" and nil when the map is empty (nothing to write).
func writeSiblingManifest(boardDir string, siblingMounts map[string]map[string]string) (string, error) {
	if len(siblingMounts) == 0 {
		return "", nil
	}
	path := filepath.Join(boardDir, "sibling_worktrees.json")
	data, err := json.MarshalIndent(siblingMounts, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// buildAgentCmd returns the standard agent Cmd slice for the given prompt and
// optional model. All sub-agent invocations follow this pattern:
//
//	-p <prompt> --verbose --output-format stream-json [--model <model>]
func buildAgentCmd(prompt, model string) []string {
	cmd := []string{"-p", prompt, "--verbose", "--output-format", "stream-json"}
	if model != "" {
		cmd = append(cmd, "--model", model)
	}
	return cmd
}

// resolveEnvFile returns the env-file path to hand the sandbox backend,
// guarding against a configured path that has vanished by launch time.
//
// The configured envFile may be overridden (via ENV_FILE / --env-file) to a
// transient location — most notably a mktemp path under /var/folders that
// macOS's periodic tmp-reaper purges after ~3 idle days. A long-idle scheduled
// task that fires after that window would otherwise fail to read its
// credentials. When the configured path is missing but the canonical default
// (<configDir>/.env) exists, fall back to it.
//
// The fallback only *redirects* to a known-good default; it never silently
// suppresses a configured path, so an unrelated missing env file still reaches
// the backend unchanged (preserving prior pass-through behavior and letting
// the backend surface its own diagnostic). Returns "" only when envFile itself
// is empty.
func (r *Runner) resolveEnvFile() string {
	if r.envFile == "" {
		return ""
	}
	if workspace.FileExists(r.envFile) {
		return r.envFile
	}
	if r.defaultEnvFile != "" && r.defaultEnvFile != r.envFile && workspace.FileExists(r.defaultEnvFile) {
		logger.Runner.Warn("configured env file missing; falling back to default",
			"configured", r.envFile, "fallback", r.defaultEnvFile)
		return r.defaultEnvFile
	}
	return r.envFile
}

// buildBaseContainerSpec creates the launch spec shared by every agent run:
// its name, the env file and the harness that runs it. Callers set the
// working directory and argv.
func (r *Runner) buildBaseContainerSpec(containerName string, sb harness.ID) executor.ContainerSpec {
	spec := executor.ContainerSpec{Name: containerName}
	if envFile := r.resolveEnvFile(); envFile != "" {
		spec.EnvFile = envFile
	}
	spec.Env = map[string]string{"WALLFACER_AGENT": string(sb)}
	return spec
}

// sandboxForTask returns the resolved sandbox type for the task's implementation activity.
// Shorthand for sandboxForTaskActivity(task, activityImplementation).
func (r *Runner) sandboxForTask(task *store.Task) harness.ID {
	return r.sandboxForTaskActivity(task, activityImplementation)
}

// sandboxForTaskActivity resolves the sandbox type for a given task and activity.
// Resolution priority: per-task per-activity override → per-task sandbox → env-file
// per-activity setting → env-file default sandbox → the configured default harness
// (harness.Default(), the final fallback). Keying the fallback on Default() rather
// than a literal harness.Claude is what lets the native default change without
// touching this resolver. A tier that names no registered harness is skipped
// and the next tier decides.
func (r *Runner) sandboxForTaskActivity(task *store.Task, activity store.SandboxActivity) harness.ID {
	if task == nil {
		return harness.Default()
	}
	activity = store.SandboxActivity(strings.ToLower(strings.TrimSpace(string(activity))))
	if task.SandboxByActivity != nil {
		if sb, ok := task.SandboxByActivity[activity]; ok && sb.IsValid() {
			return sb
		}
	}
	if task.Sandbox.IsValid() {
		return task.Sandbox
	}
	if sb := r.sandboxFromEnvForActivity(activity); sb.IsValid() {
		return sb
	}
	return harness.Default()
}

// sandboxFromEnvForActivity reads the env-file sandbox routing for a specific activity.
// Falls back to cfg.DefaultSandbox when no activity-specific override is set.
// Returns "" when the env file is absent or unparseable.
func (r *Runner) sandboxFromEnvForActivity(activity store.SandboxActivity) harness.ID {
	if r.envFile == "" {
		return ""
	}
	cfg, err := envconfig.Parse(r.envFile)
	if err != nil {
		return ""
	}
	activity = store.SandboxActivity(strings.ToLower(strings.TrimSpace(string(activity))))
	switch activity {
	case activityImplementation:
		if cfg.ImplementationSandbox != "" {
			return cfg.ImplementationSandbox
		}
	case activityOversight:
		if cfg.OversightSandbox != "" {
			return cfg.OversightSandbox
		}
	case activityCommitMessage:
		if cfg.CommitMessageSandbox != "" {
			return cfg.CommitMessageSandbox
		}
	}
	return cfg.DefaultSandbox
}

// modelFromEnvForSandbox reads the default model for the given sandbox.
// Supports "claude" and "codex" values.
func (r *Runner) modelFromEnvForSandbox(sb harness.ID) string {
	if r.envFile == "" {
		return ""
	}
	cfg, err := envconfig.Parse(r.envFile)
	if err != nil {
		return ""
	}
	switch sb {
	case harness.Codex:
		return cfg.CodexDefaultModel
	default:
		return cfg.DefaultModel
	}
}

// roleImplementation is the heavyweight descriptor the multi-turn agent
// turn loop in execute.go calls runAgent through. It carries no timeout
// (the turn loop owns the deadline via ctx), no ParseResult (the caller
// operates directly on the raw agentOutput), and a noop ParseResult stub
// so runAgent's required-field check passes. Turn sequencing and session
// recovery stay in execute.go; runAgent handles only the per-turn launch.
// roleImplementation binds to the descriptor in the internal/agents
// package; the runner's dispatch plumbing lives in agent_bindings.go.
var roleImplementation = agents.Implementation

// runContainer executes an agent container and parses its NDJSON output.
// Returns (output, rawStdout, rawStderr, error). Wraps runAgent with the
// heavyweight-specific concerns: slugged container name, live-log tee,
// container-runtime circuit breaker, and the per-activity descriptor
// dispatch. The outer turn loop in execute.go owns session handling
// and retry policy beyond the Claude→Codex token-limit fallback.
func (r *Runner) runContainer(
	ctx context.Context,
	taskID uuid.UUID,
	prompt, sessionID string,
	worktreeOverrides map[string]string,
	boardDir string,
	siblingMounts map[string]map[string]string,
	modelOverride string,
	activity store.SandboxActivity,
) (*agentOutput, []byte, []byte, error) {
	slug := slugifyPrompt(prompt, 30)
	containerName := "wallfacer-" + slug + "-" + taskID.String()[:8]

	task, err := r.taskStore(taskID).GetTask(r.shutdownCtx, taskID)
	if err != nil {
		logger.Runner.Warn("runContainer: get task", "task", taskID, "error", err)
	}

	role := roleImplementation

	// Set up the live-log buffer that StreamLogs attaches to while the
	// container is running. The tee is wired via LiveLogWriter so
	// runAgent drains both streams through it.
	ll := livelog.New()
	r.liveLogs.Store(taskID, ll)
	defer func() {
		ll.Close()
		r.liveLogs.Delete(taskID)
	}()

	defer r.taskProcesses.Delete(taskID)

	res, err := r.runAgent(ctx, role, task, prompt, runAgentOpts{
		ContainerName:     containerName,
		SessionID:         sessionID,
		ModelOverride:     modelOverride,
		WorktreeOverrides: worktreeOverrides,
		BoardDir:          boardDir,
		SiblingMounts:     siblingMounts,
		LiveLogWriter:     ll,
		CircuitBreaker:    r.containerCB,
		EmitSpanEvents:    true,
		// Register the process so KillContainer can signal the running
		// agent when the user cancels the task mid-run.
		OnLaunch: func(_ string, handle executor.Handle) {
			r.taskProcesses.Store(taskID, handle)
		},
		// Heavyweight turn invocations rebind the activity bucket
		// for each turn's usage ledger — implementation or testing.
		ActivityOverride: activity,
		// Usage is accounted in the outer turn-loop; runAgent does not
		// bill heavyweight turns itself because the loop already does.
	})
	var output *agentOutput
	var rawStdout, rawStderr []byte
	if res != nil {
		output = res.Output
		rawStdout = res.RawStdout
		rawStderr = res.RawStderr
	}
	if err != nil {
		// Retry with codex on a token/rate limit. The first launch
		// already consumed the primary sandbox via the role's
		// sandboxForTaskActivity resolution, so the fallback is a
		// separate runAgent call that hard-forces Codex by pinning
		// the model override.
		if task != nil && r.sandboxForTaskActivity(task, activity) == harness.Claude &&
			isLikelyTokenLimitError(err.Error(), string(rawStderr)) {
			logger.Runner.Warn("claude sandbox token limit hit; retrying with codex",
				"task", taskID, "activity", activity)
			_ = r.taskStore(taskID).InsertEvent(ctx, taskID, store.EventTypeSystem, map[string]string{
				"result": "Sandbox fallback: claude → codex (token/rate limit hit)",
			})
			return r.runContainerOnSandbox(ctx, role, taskID, task, containerName, prompt, sessionID,
				modelOverride, worktreeOverrides, boardDir, siblingMounts, ll, harness.Codex)
		}
		return nil, rawStdout, rawStderr, err
	}
	if task != nil && r.sandboxForTaskActivity(task, activity) == harness.Claude &&
		output != nil && output.IsError && isLikelyTokenLimitError(output.Result, output.Subtype) {
		logger.Runner.Warn("claude sandbox reported token limit in output; retrying with codex",
			"task", taskID, "activity", activity)
		_ = r.taskStore(taskID).InsertEvent(ctx, taskID, store.EventTypeSystem, map[string]string{
			"result": "Sandbox fallback: claude → codex (token/rate limit in output)",
		})
		return r.runContainerOnSandbox(ctx, role, taskID, task, containerName, prompt, sessionID,
			modelOverride, worktreeOverrides, boardDir, siblingMounts, ll, harness.Codex)
	}

	return output, rawStdout, rawStderr, nil
}

// runContainerOnSandbox is the inner codex-fallback helper. It re-runs
// the heavyweight launch against a pinned sandbox type by forcing
// the model override to the sandbox's env-derived default. Used from
// runContainer when the first attempt surfaced a token/rate limit.
func (r *Runner) runContainerOnSandbox(
	ctx context.Context,
	role AgentRole,
	taskID uuid.UUID,
	task *store.Task,
	containerName, prompt, sessionID, modelOverride string,
	worktreeOverrides map[string]string,
	boardDir string,
	siblingMounts map[string]map[string]string,
	ll *livelog.Log,
	sb harness.ID,
) (*agentOutput, []byte, []byte, error) {
	// Override the per-activity sandbox resolution by temporarily
	// assigning Sandbox on a shallow task copy so sandboxForTaskActivity
	// returns the pinned sandbox.
	var taskCopy *store.Task
	if task != nil {
		c := *task
		c.Sandbox = sb
		c.SandboxByActivity = nil
		taskCopy = &c
	}
	res, err := r.runAgent(ctx, role, taskCopy, prompt, runAgentOpts{
		ContainerName:     containerName,
		SessionID:         sessionID,
		ModelOverride:     modelOverride,
		WorktreeOverrides: worktreeOverrides,
		BoardDir:          boardDir,
		SiblingMounts:     siblingMounts,
		LiveLogWriter:     ll,
		CircuitBreaker:    r.containerCB,
		EmitSpanEvents:    true,
		// Register the fallback launch's handle too, so a cancel during
		// the codex-fallback turn can still kill the running agent.
		OnLaunch: func(_ string, handle executor.Handle) {
			r.taskProcesses.Store(taskID, handle)
		},
	})
	if res == nil {
		return nil, nil, nil, err
	}
	return res.Output, res.RawStdout, res.RawStderr, err
}

// isLikelyTokenLimitError heuristically detects rate-limit and token-limit errors
// by scanning the joined lowercase text for known keyword groups. Used to trigger
// claude→codex sandbox fallback when the claude sandbox hits API limits.
func isLikelyTokenLimitError(parts ...string) bool {
	joined := strings.ToLower(strings.Join(parts, " "))
	if joined == "" {
		return false
	}
	// Each entry is a group of keywords that must ALL appear in the text.
	// This is more resilient to phrasing variations than exact substring matching.
	keywordGroups := [][]string{
		{"hit", "limit"},
		{"rate", "limit"},
		{"token", "limit"},
		{"too many", "token"},
		{"quota"},
		{"insufficient", "credit"},
		{"credit", "too low"},
		{"context", "length"},
		{"prompt", "too long"},
	}
	for _, group := range keywordGroups {
		match := true
		for _, kw := range group {
			if !strings.Contains(joined, kw) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
