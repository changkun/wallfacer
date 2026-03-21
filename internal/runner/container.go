package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"changkun.de/wallfacer/internal/envconfig"
	"changkun.de/wallfacer/internal/instructions"
	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/sandbox"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// agentUsage mirrors the token-usage object in the agent's JSON output.
type agentUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// agentOutput is the top-level result object emitted by the agent container
// (either as a single JSON blob or as the last line of NDJSON stream-json).
type agentOutput struct {
	Result       string     `json:"result"`
	SessionID    string     `json:"session_id"`
	ThreadID     string     `json:"thread_id,omitempty"`
	StopReason   string     `json:"stop_reason"`
	Subtype      string     `json:"subtype"`
	IsError      bool       `json:"is_error"`
	TotalCostUSD float64    `json:"total_cost_usd"`
	Usage        agentUsage `json:"usage"`

	// ActualSandbox is set by runContainer (not parsed from JSON) to record
	// which sandbox actually executed this turn, including fallback scenarios.
	ActualSandbox sandbox.Type `json:"-"`
}

const (
	activityImplementation = store.SandboxActivityImplementation
	activityTesting        = store.SandboxActivityTesting
	activityRefinement     = store.SandboxActivityRefinement
	activityTitle          = store.SandboxActivityTitle
	activityOversight      = store.SandboxActivityOversight
	activityCommitMessage  = store.SandboxActivityCommitMessage
	activityIdeaAgent      = store.SandboxActivityIdeaAgent
)

// buildContainerArgs constructs the full argument list for the container run command.
// It is a pure function of runner configuration and the supplied parameters,
// which makes it easy to unit-test without actually launching a container.
//
// taskID, when non-empty, is used to label the container with wallfacer.task.id
// so the monitor can correlate containers to tasks even with slug-based names.
// boardDir, when non-empty, is a host directory containing board.json that
// will be mounted read-only at /workspace/.tasks/ inside the container.
// siblingMounts maps shortID → (repoPath → worktreePath) for read-only
// sibling worktree mounts under /workspace/.tasks/worktrees/.
func (r *Runner) buildContainerArgs(
	containerName, taskID, prompt, sessionID string,
	worktreeOverrides map[string]string,
	boardDir string,
	siblingMounts map[string]map[string]string,
	modelOverride string,
) []string {
	return r.buildContainerArgsForSandbox(containerName, taskID, prompt, sessionID, worktreeOverrides, boardDir, siblingMounts, modelOverride, sandbox.Claude)
}

func (r *Runner) buildContainerArgsForSandbox(
	containerName, taskID, prompt, sessionID string,
	worktreeOverrides map[string]string,
	boardDir string,
	siblingMounts map[string]map[string]string,
	modelOverride string,
	sb sandbox.Type,
) []string {
	// Resolve model once: override takes priority, then env default.
	model := modelOverride
	if model == "" {
		model = r.modelFromEnvForSandbox(sb)
	}

	spec := r.buildBaseContainerSpec(containerName, model, sb)

	// Label the container with task metadata so the monitor can correlate
	// containers to tasks by label rather than by parsing the container name.
	if taskID != "" {
		spec.Labels = map[string]string{
			"wallfacer.task.id":     taskID,
			"wallfacer.task.prompt": truncate(prompt, 80),
		}
	}

	// Mount workspaces, substituting per-task worktree paths where available.
	var basenames []string
	if r.workspaces != "" {
		for _, ws := range strings.Fields(r.workspaces) {
			ws = strings.TrimSpace(ws)
			if ws == "" {
				continue
			}
			hostPath := ws
			if wt, ok := worktreeOverrides[ws]; ok {
				hostPath = wt
			}
			parts := strings.Split(ws, "/")
			basename := parts[len(parts)-1]
			if basename == "" && len(parts) > 1 {
				basename = parts[len(parts)-2]
			}
			basenames = append(basenames, basename)
			spec.Volumes = append(spec.Volumes, VolumeMount{
				Host:      hostPath,
				Container: "/workspace/" + basename,
				Options:   "z",
			})

			// Git worktrees have a .git file (not directory) that references
			// the main repo's .git/worktrees/<name>/ using an absolute host
			// path. Mount the main repo's .git directory at the same host
			// path inside the container so git operations work correctly.
			if _, isWorktree := worktreeOverrides[ws]; isWorktree {
				gitDir := filepath.Join(ws, ".git")
				if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
					spec.Volumes = append(spec.Volumes, VolumeMount{
						Host:      gitDir,
						Container: gitDir,
						Options:   "z",
					})
				}
			}
		}
	}

	// Mount workspace-level instructions file based on sandbox convention:
	// - Claude sandbox expects /workspace/CLAUDE.md
	// - Codex sandbox expects /workspace/AGENTS.md
	spec.Volumes = r.appendInstructionsMount(spec.Volumes, sb)

	// Board context: mount board.json read-only at /workspace/.tasks/.
	if boardDir != "" {
		spec.Volumes = append(spec.Volumes, VolumeMount{
			Host:      boardDir,
			Container: "/workspace/.tasks",
			Options:   "z,ro",
		})
	}

	// Sibling worktrees: mount each eligible sibling's worktrees read-only.
	// Sort by shortID then by repoPath for deterministic output.
	shortIDs := make([]string, 0, len(siblingMounts))
	for shortID := range siblingMounts {
		shortIDs = append(shortIDs, shortID)
	}
	sort.Strings(shortIDs)
	for _, shortID := range shortIDs {
		repos := siblingMounts[shortID]
		repoPaths := make([]string, 0, len(repos))
		for repoPath := range repos {
			repoPaths = append(repoPaths, repoPath)
		}
		sort.Strings(repoPaths)
		for _, repoPath := range repoPaths {
			wtPath := repos[repoPath]
			basename := filepath.Base(repoPath)
			containerPath := "/workspace/.tasks/worktrees/" + shortID + "/" + basename
			spec.Volumes = append(spec.Volumes, VolumeMount{
				Host:      wtPath,
				Container: containerPath,
				Options:   "z,ro",
			})
		}
	}

	// When there is exactly one workspace, set CWD directly into it so
	// Claude operates in the repo directory by default. For multiple
	// workspaces keep CWD at /workspace so all repos are accessible.
	spec.WorkDir = workdirForBasenames(basenames)

	// Build the agent command: prompt, verbosity flags, optional model, optional resume.
	spec.Cmd = buildAgentCmd(prompt, model)
	if sessionID != "" {
		spec.Cmd = append(spec.Cmd, "--resume", sessionID)
	}

	spec.Network = r.resolvedContainerNetwork()
	return spec.Build()
}

func instructionsFilenameForSandbox(sb sandbox.Type) string {
	if sb == sandbox.Codex {
		return instructions.InstructionsFilename
	}
	return instructions.LegacyInstructionsFilename
}

// appendInstructionsMount adds the workspace-level instructions file as a
// read-only bind mount (CLAUDE.md for claude, AGENTS.md for codex).
// It is a no-op when instructionsPath is empty or does not exist on the host.
// Both buildContainerArgsForSandbox and buildIdeationContainerArgs share this logic.
func (r *Runner) appendInstructionsMount(volumes []VolumeMount, sb sandbox.Type) []VolumeMount {
	if r.instructionsPath == "" {
		return volumes
	}
	if _, err := os.Stat(r.instructionsPath); err != nil {
		return volumes
	}
	return append(volumes, VolumeMount{
		Host:      r.instructionsPath,
		Container: "/workspace/" + instructionsFilenameForSandbox(sb),
		Options:   "z,ro",
	})
}

// workdirForBasenames returns the container working directory for the given set
// of workspace basenames. A single workspace sets CWD into that workspace;
// multiple workspaces keep CWD at /workspace so all repos are accessible.
func workdirForBasenames(basenames []string) string {
	if len(basenames) == 1 {
		return "/workspace/" + basenames[0]
	}
	return "/workspace"
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

func (r *Runner) appendCodexAuthMount(volumes []VolumeMount, sb sandbox.Type) []VolumeMount {
	if sb != sandbox.Codex {
		return volumes
	}
	if hostPath := r.hostCodexAuthPath(); hostPath != "" {
		volumes = append(volumes, VolumeMount{
			Host:      hostPath,
			Container: "/home/codex/.codex",
			Options:   "z,ro",
		})
	}
	return volumes
}

// buildBaseContainerSpec creates a ContainerSpec pre-populated with the
// configuration shared across all sub-agent invocations:
//   - Runtime, Name, and Image resolved from the configured sandbox
//   - EnvFile (when configured)
//   - CLAUDE_CODE_MODEL environment variable (when model is non-empty)
//   - claude-config named volume for agent configuration persistence
//   - Codex auth bind-mount (when sandbox=="codex" and the path exists on the host)
//
// Callers set Labels, additional Volumes (workspace directories, instructions
// file, board context), WorkDir, and Cmd for their specific needs.
func (r *Runner) buildBaseContainerSpec(containerName, model string, sb sandbox.Type) ContainerSpec {
	spec := ContainerSpec{
		Runtime: r.command,
		Name:    containerName,
		Image:   r.sandboxImageForSandbox(sb),
	}
	if r.envFile != "" {
		spec.EnvFile = r.envFile
	}
	if model != "" {
		spec.Env = map[string]string{"CLAUDE_CODE_MODEL": model}
	}
	spec.Volumes = append(spec.Volumes, VolumeMount{
		Host:      "claude-config",
		Container: "/home/claude/.claude",
	})
	spec.Volumes = r.appendCodexAuthMount(spec.Volumes, sb)
	spec.Network = r.resolvedContainerNetwork()
	spec.CPUs = r.resolvedContainerCPUs()
	spec.Memory = r.resolvedContainerMemory()
	return spec
}

func (r *Runner) sandboxImageForSandbox(sb sandbox.Type) string {
	if sb != sandbox.Codex {
		return strings.TrimSpace(r.sandboxImage)
	}
	baseImage := strings.TrimSpace(r.sandboxImage)
	if baseImage == "" {
		return "wallfacer-codex:latest"
	}
	low := strings.ToLower(baseImage)
	if strings.Contains(low, "wallfacer-codex") {
		return baseImage
	}
	registry := baseImage
	digest := ""
	if at := strings.Index(registry, "@"); at != -1 {
		digest = registry[at:]
		registry = registry[:at]
	}
	tag := ""
	if at := strings.LastIndex(registry, ":"); at != -1 {
		tag = registry[at:]
		registry = registry[:at]
	}
	prefix := ""
	repoName := registry
	if idx := strings.LastIndex(repoName, "/"); idx != -1 {
		prefix = repoName[:idx+1]
		repoName = repoName[idx+1:]
	}
	if repoName != "wallfacer" {
		return baseImage
	}
	return prefix + "wallfacer-codex" + tag + digest
}

// modelFromEnv reads CLAUDE_DEFAULT_MODEL from the env file (if configured).
// Returns an empty string when the file cannot be read or the key is absent.
func (r *Runner) sandboxForTask(task *store.Task) sandbox.Type {
	return r.sandboxForTaskActivity(task, activityImplementation)
}

func (r *Runner) sandboxForTaskActivity(task *store.Task, activity store.SandboxActivity) sandbox.Type {
	if task == nil {
		return sandbox.Claude
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
	if sb := r.sandboxFromEnvForActivity(activity); sb != "" {
		return sb
	}
	return sandbox.Claude
}

func (r *Runner) sandboxFromEnvForActivity(activity store.SandboxActivity) sandbox.Type {
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
	case activityTesting:
		if cfg.TestingSandbox != "" {
			return cfg.TestingSandbox
		}
	case activityRefinement:
		if cfg.RefinementSandbox != "" {
			return cfg.RefinementSandbox
		}
	case activityTitle:
		if cfg.TitleSandbox != "" {
			return cfg.TitleSandbox
		}
	case activityOversight:
		if cfg.OversightSandbox != "" {
			return cfg.OversightSandbox
		}
	case activityCommitMessage:
		if cfg.CommitMessageSandbox != "" {
			return cfg.CommitMessageSandbox
		}
	case activityIdeaAgent:
		if cfg.IdeaAgentSandbox != "" {
			return cfg.IdeaAgentSandbox
		}
	}
	return cfg.DefaultSandbox
}

func (r *Runner) modelFromEnv() string {
	return r.modelFromEnvForSandbox(sandbox.Claude)
}

// modelFromEnvForSandbox reads the default model for the given sandbox.
// Supports "claude" and "codex" values.
func (r *Runner) modelFromEnvForSandbox(sb sandbox.Type) string {
	if r.envFile == "" {
		return ""
	}
	cfg, err := envconfig.Parse(r.envFile)
	if err != nil {
		return ""
	}
	switch sb {
	case sandbox.Codex:
		return cfg.CodexDefaultModel
	default:
		return cfg.DefaultModel
	}
}

// titleModelFromEnv reads CLAUDE_TITLE_MODEL from the env file,
// falling back to CLAUDE_DEFAULT_MODEL if the title model is not set.
func (r *Runner) titleModelFromEnv() string {
	return r.titleModelFromEnvForSandbox(sandbox.Claude)
}

// titleModelFromEnvForSandbox returns the sandbox-specific title model.
// Supports "claude" and "codex" values.
func (r *Runner) titleModelFromEnvForSandbox(sb sandbox.Type) string {
	if r.envFile == "" {
		return ""
	}
	cfg, err := envconfig.Parse(r.envFile)
	if err != nil {
		return ""
	}
	switch sb {
	case sandbox.Codex:
		if cfg.CodexTitleModel != "" {
			return cfg.CodexTitleModel
		}
		return cfg.CodexDefaultModel
	default:
		if cfg.TitleModel != "" {
			return cfg.TitleModel
		}
		return cfg.DefaultModel
	}
}

// runContainer executes an agent container and parses its NDJSON output.
// Returns (output, rawStdout, rawStderr, error).
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
	// Build a human-readable container name: wallfacer-<slug>-<uuid8>
	// The slug is derived from the task prompt so external tools (docker ps,
	// podman ps) can identify which task is running without needing the UUID.
	slug := slugifyPrompt(prompt, 30)
	containerName := "wallfacer-" + slug + "-" + taskID.String()[:8]

	// Track the container name so KillContainer and StreamLogs can find it.
	r.taskContainers.Set(taskID, containerName)
	defer r.taskContainers.Delete(taskID)

	sb := sandbox.Claude
	if task, err := r.store.GetTask(r.shutdownCtx, taskID); err == nil {
		sb = r.sandboxForTaskActivity(task, activity)
	} else {
		logger.Runner.Warn("runContainer: get task", "task", taskID, "error", err)
	}

	runWithSandbox := func(selectedSandbox sandbox.Type) (*agentOutput, []byte, []byte, error) {
		// Refuse to launch if the container runtime is known-unavailable.
		if !r.containerCB.Allow() {
			return nil, nil, nil, fmt.Errorf("container circuit breaker open: container runtime may be unavailable")
		}

		args := r.buildContainerArgsForSandbox(containerName, taskID.String(), prompt, sessionID, worktreeOverrides, boardDir, siblingMounts, modelOverride, selectedSandbox)

		logger.Runner.Debug("exec", "cmd", r.command, "args", strings.Join(args, " "), "sandbox", selectedSandbox)
		_ = r.store.InsertEvent(ctx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "container_run", Label: string(activity)})

		rawStdout, rawStderr, runErr := r.executor.RunArgs(ctx, containerName, args)
		_ = r.store.InsertEvent(ctx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "container_run", Label: string(activity)})


		// Detect container runtime failures (daemon/binary unavailable).
		// Only trip the breaker for runtime-level errors, not for Claude
		// exiting non-zero (exit codes 1–124).
		if runErr != nil && ctx.Err() == nil && isContainerRuntimeError(runErr) {
			r.containerCB.RecordFailure()
		}

		// If the context was cancelled or timed out, kill the container explicitly
		// and return the context error rather than parsing potentially incomplete output.
		if ctx.Err() != nil {
			r.executor.Kill(containerName)
			return nil, rawStdout, rawStderr, fmt.Errorf("container terminated: %w", ctx.Err())
		}

		raw := strings.TrimSpace(string(rawStdout))
		if raw == "" {
			if runErr != nil {
				if exitErr, ok := runErr.(*exec.ExitError); ok {
					return nil, rawStdout, rawStderr,
						fmt.Errorf("container exited with code %d: stderr=%s", exitErr.ExitCode(), string(rawStderr))
				}
				return nil, rawStdout, rawStderr, fmt.Errorf("exec container: %w", runErr)
			}
			stderrStr := strings.TrimSpace(string(rawStderr))
			if stderrStr != "" {
				return nil, rawStdout, rawStderr,
					fmt.Errorf("empty output from container: stderr=%s", truncate(stderrStr, 500))
			}
			return nil, rawStdout, rawStderr, fmt.Errorf("empty output from container")
		}

		output, parseErr := parseOutput(raw)
		if parseErr != nil {
			if runErr != nil {
				if exitErr, ok := runErr.(*exec.ExitError); ok {
					return nil, rawStdout, rawStderr,
						fmt.Errorf("container exited with code %d: stderr=%s stdout=%s",
							exitErr.ExitCode(), string(rawStderr), truncate(raw, 500))
				}
				return nil, rawStdout, rawStderr, fmt.Errorf("exec container: %w", runErr)
			}
			return nil, rawStdout, rawStderr,
				fmt.Errorf("parse output: %w (raw: %s)", parseErr, truncate(raw, 200))
		}

		// The agent may exit non-zero even when it produces a valid result.
		if runErr != nil {
			if exitErr, ok := runErr.(*exec.ExitError); ok {
				logger.Runner.Warn("container exited non-zero but produced valid output",
					"task", taskID, "code", exitErr.ExitCode(), "sandbox", selectedSandbox)
			} else {
				logger.Runner.Warn("container error but produced valid output", "task", taskID, "error", runErr, "sandbox", selectedSandbox)
			}
		}

		// Container runtime is healthy: close the circuit (or keep it closed).
		r.containerCB.RecordSuccess()
		output.ActualSandbox = selectedSandbox
		return output, rawStdout, rawStderr, nil
	}

	output, rawStdout, rawStderr, err := runWithSandbox(sb)
	if err != nil {
		if sb == sandbox.Claude && isLikelyTokenLimitError(err.Error(), string(rawStderr)) {
			logger.Runner.Warn("claude sandbox token limit hit; retrying with codex",
				"task", taskID, "activity", activity)
			_ = r.store.InsertEvent(ctx, taskID, store.EventTypeSystem, map[string]string{

				"result": "Sandbox fallback: claude → codex (token/rate limit hit)",
			})
			return runWithSandbox(sandbox.Codex)
		}
		return nil, rawStdout, rawStderr, err
	}

	if sb == sandbox.Claude && output != nil && output.IsError &&
		isLikelyTokenLimitError(output.Result, output.Subtype) {
		logger.Runner.Warn("claude sandbox reported token limit in output; retrying with codex",
			"task", taskID, "activity", activity)
		_ = r.store.InsertEvent(ctx, taskID, store.EventTypeSystem, map[string]string{

			"result": "Sandbox fallback: claude → codex (token/rate limit in output)",
		})
		return runWithSandbox(sandbox.Codex)
	}

	return output, rawStdout, rawStderr, nil
}

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

// runGit is a helper to run a git command and discard output (best-effort).
func runGit(dir string, args ...string) error {
	return exec.Command("git", append([]string{"-C", dir}, args...)...).Run()
}
