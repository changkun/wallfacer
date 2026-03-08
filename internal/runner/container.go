package runner

import (
	"bytes"
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
	StopReason   string     `json:"stop_reason"`
	Subtype      string     `json:"subtype"`
	IsError      bool       `json:"is_error"`
	TotalCostUSD float64    `json:"total_cost_usd"`
	Usage        agentUsage `json:"usage"`
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
	return r.buildContainerArgsForSandbox(containerName, taskID, prompt, sessionID, worktreeOverrides, boardDir, siblingMounts, modelOverride, "claude")
}

func (r *Runner) buildContainerArgsForSandbox(
	containerName, taskID, prompt, sessionID string,
	worktreeOverrides map[string]string,
	boardDir string,
	siblingMounts map[string]map[string]string,
	modelOverride, sandbox string,
) []string {
	// Resolve model once: override takes priority, then env default.
	model := modelOverride
	if model == "" {
		model = r.modelFromEnvForSandbox(sandbox)
	}

	spec := ContainerSpec{
		Runtime: r.command,
		Name:    containerName,
		Image:   r.sandboxImage,
	}

	// Label the container with task metadata so the monitor can correlate
	// containers to tasks by label rather than by parsing the container name.
	if taskID != "" {
		spec.Labels = map[string]string{
			"wallfacer.task.id":     taskID,
			"wallfacer.task.prompt": truncate(prompt, 80),
		}
	}

	if r.envFile != "" {
		spec.EnvFile = r.envFile
	}

	// Inject CLAUDE_CODE_MODEL so subagent model selection also uses the
	// configured model (not just the --model CLI flag which only affects
	// the main session).
	if model != "" {
		spec.Env = map[string]string{"CLAUDE_CODE_MODEL": model}
	}

	// Mount agent config volume.
	spec.Volumes = append(spec.Volumes, VolumeMount{
		Host:      "claude-config",
		Container: "/home/claude/.claude",
	})

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
	if r.instructionsPath != "" {
		if _, err := os.Stat(r.instructionsPath); err == nil {
			spec.Volumes = append(spec.Volumes, VolumeMount{
				Host:      r.instructionsPath,
				Container: "/workspace/" + instructionsFilenameForSandbox(sandbox),
				Options:   "z,ro",
			})
		}
	}

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
	workdir := "/workspace"
	if len(basenames) == 1 {
		workdir = "/workspace/" + basenames[0]
	}
	spec.WorkDir = workdir

	// Build the agent command: prompt, verbosity flags, optional model, optional resume.
	spec.Cmd = []string{"-p", prompt, "--verbose", "--output-format", "stream-json"}
	if model != "" {
		spec.Cmd = append(spec.Cmd, "--model", model)
	}
	if sessionID != "" {
		spec.Cmd = append(spec.Cmd, "--resume", sessionID)
	}

	return spec.Build()
}

func instructionsFilenameForSandbox(sandbox string) string {
	if strings.EqualFold(strings.TrimSpace(sandbox), "codex") {
		return instructions.InstructionsFilename
	}
	return instructions.LegacyInstructionsFilename
}

// modelFromEnv reads CLAUDE_DEFAULT_MODEL from the env file (if configured).
// Returns an empty string when the file cannot be read or the key is absent.
func (r *Runner) sandboxForTask(task *store.Task) string {
	return r.sandboxForTaskActivity(task, activityImplementation)
}

func (r *Runner) sandboxForTaskActivity(task *store.Task, activity string) string {
	if task == nil {
		return "claude"
	}
	activity = strings.ToLower(strings.TrimSpace(activity))
	if task.SandboxByActivity != nil {
		if sandbox := strings.ToLower(strings.TrimSpace(task.SandboxByActivity[activity])); sandbox != "" {
			return sandbox
		}
	}
	if sandbox := strings.ToLower(strings.TrimSpace(task.Sandbox)); sandbox != "" {
		return sandbox
	}
	if sandbox := r.sandboxFromEnvForActivity(activity); sandbox != "" {
		return sandbox
	}
	return "claude"
}

func (r *Runner) sandboxFromEnvForActivity(activity string) string {
	if r.envFile == "" {
		return ""
	}
	cfg, err := envconfig.Parse(r.envFile)
	if err != nil {
		return ""
	}
	activity = strings.ToLower(strings.TrimSpace(activity))
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
	return strings.ToLower(strings.TrimSpace(cfg.DefaultSandbox))
}

func (r *Runner) modelFromEnv() string {
	return r.modelFromEnvForSandbox("claude")
}

// modelFromEnvForSandbox reads the default model for the given sandbox.
// Supports "claude" and "codex" values.
func (r *Runner) modelFromEnvForSandbox(sandbox string) string {
	if r.envFile == "" {
		return ""
	}
	cfg, err := envconfig.Parse(r.envFile)
	if err != nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(sandbox)) {
	case "codex":
		return cfg.CodexDefaultModel
	default:
		return cfg.DefaultModel
	}
}

// titleModelFromEnv reads CLAUDE_TITLE_MODEL from the env file,
// falling back to CLAUDE_DEFAULT_MODEL if the title model is not set.
func (r *Runner) titleModelFromEnv() string {
	return r.titleModelFromEnvForSandbox("claude")
}

// titleModelFromEnvForSandbox returns the sandbox-specific title model.
// Supports "claude" and "codex" values.
func (r *Runner) titleModelFromEnvForSandbox(sandbox string) string {
	if r.envFile == "" {
		return ""
	}
	cfg, err := envconfig.Parse(r.envFile)
	if err != nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(sandbox)) {
	case "codex":
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
	activity string,
) (*agentOutput, []byte, []byte, error) {
	// Build a human-readable container name: wallfacer-<slug>-<uuid8>
	// The slug is derived from the task prompt so external tools (docker ps,
	// podman ps) can identify which task is running without needing the UUID.
	slug := slugifyPrompt(prompt, 30)
	containerName := "wallfacer-" + slug + "-" + taskID.String()[:8]

	// Track the container name so KillContainer and StreamLogs can find it.
	r.taskContainers.Set(taskID, containerName)
	defer r.taskContainers.Delete(taskID)

	// Remove any leftover container from a previous interrupted run.
	exec.Command(r.command, "rm", "-f", containerName).Run()

	sandbox := "claude"
	if task, err := r.store.GetTask(context.Background(), taskID); err == nil {
		sandbox = r.sandboxForTaskActivity(task, activity)
	} else {
		logger.Runner.Warn("runContainer: get task", "task", taskID, "error", err)
	}

	runWithSandbox := func(selectedSandbox string) (*agentOutput, []byte, []byte, error) {
		args := r.buildContainerArgsForSandbox(containerName, taskID.String(), prompt, sessionID, worktreeOverrides, boardDir, siblingMounts, modelOverride, selectedSandbox)

		cmd := exec.CommandContext(ctx, r.command, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		logger.Runner.Debug("exec", "cmd", r.command, "args", strings.Join(args, " "), "sandbox", selectedSandbox)
		r.store.InsertEvent(ctx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "container_run", Label: activity})
		runErr := cmd.Run()
		r.store.InsertEvent(ctx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "container_run", Label: activity})

		// If the context was cancelled or timed out, kill the container explicitly
		// and return the context error rather than parsing potentially incomplete output.
		if ctx.Err() != nil {
			exec.Command(r.command, "kill", containerName).Run()
			exec.Command(r.command, "rm", "-f", containerName).Run()
			return nil, stdout.Bytes(), stderr.Bytes(), fmt.Errorf("container terminated: %w", ctx.Err())
		}

		raw := strings.TrimSpace(stdout.String())
		if raw == "" {
			if runErr != nil {
				if exitErr, ok := runErr.(*exec.ExitError); ok {
					return nil, stdout.Bytes(), stderr.Bytes(),
						fmt.Errorf("container exited with code %d: stderr=%s", exitErr.ExitCode(), stderr.String())
				}
				return nil, stdout.Bytes(), stderr.Bytes(), fmt.Errorf("exec container: %w", runErr)
			}
			stderrStr := strings.TrimSpace(stderr.String())
			if stderrStr != "" {
				return nil, stdout.Bytes(), stderr.Bytes(),
					fmt.Errorf("empty output from container: stderr=%s", truncate(stderrStr, 500))
			}
			return nil, stdout.Bytes(), stderr.Bytes(), fmt.Errorf("empty output from container")
		}

		output, parseErr := parseOutput(raw)
		if parseErr != nil {
			if runErr != nil {
				if exitErr, ok := runErr.(*exec.ExitError); ok {
					return nil, stdout.Bytes(), stderr.Bytes(),
						fmt.Errorf("container exited with code %d: stderr=%s stdout=%s",
							exitErr.ExitCode(), stderr.String(), truncate(raw, 500))
				}
				return nil, stdout.Bytes(), stderr.Bytes(), fmt.Errorf("exec container: %w", runErr)
			}
			return nil, stdout.Bytes(), stderr.Bytes(),
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

		return output, stdout.Bytes(), stderr.Bytes(), nil
	}

	output, rawStdout, rawStderr, err := runWithSandbox(sandbox)
	if err != nil {
		if strings.EqualFold(sandbox, "claude") && isLikelyTokenLimitError(err.Error(), string(rawStderr)) {
			logger.Runner.Warn("claude sandbox token limit hit; retrying with codex",
				"task", taskID, "activity", activity)
			return runWithSandbox("codex")
		}
		return nil, rawStdout, rawStderr, err
	}

	if strings.EqualFold(sandbox, "claude") && output != nil && output.IsError &&
		isLikelyTokenLimitError(output.Result, output.Subtype) {
		logger.Runner.Warn("claude sandbox reported token limit in output; retrying with codex",
			"task", taskID, "activity", activity)
		return runWithSandbox("codex")
	}

	return output, rawStdout, rawStderr, nil
}

func isLikelyTokenLimitError(parts ...string) bool {
	joined := strings.ToLower(strings.Join(parts, " "))
	if joined == "" {
		return false
	}
	needles := []string{
		"token limit",
		"rate limit",
		"quota",
		"insufficient credits",
		"credit balance is too low",
		"exceeded your current quota",
		"too many tokens",
		"maximum context length",
		"context length",
		"prompt is too long",
	}
	for _, n := range needles {
		if strings.Contains(joined, n) {
			return true
		}
	}
	return false
}

// runGit is a helper to run a git command and discard output (best-effort).
func runGit(dir string, args ...string) error {
	return exec.Command("git", append([]string{"-C", dir}, args...)...).Run()
}
