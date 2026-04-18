package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"changkun.de/x/wallfacer/internal/envconfig"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/prompts"
	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// mountOpts returns volume mount options appropriate for the host OS.
// The "z" SELinux relabeling option is only included on Linux.
func mountOpts(opts ...string) string {
	if runtime.GOOS != "linux" {
		filtered := make([]string, 0, len(opts))
		for _, o := range opts {
			if o != "z" {
				filtered = append(filtered, o)
			}
		}
		return strings.Join(filtered, ",")
	}
	return strings.Join(opts, ",")
}

// agentUsage mirrors the token-usage JSON object emitted by the agent container.
// Fields map directly to the Anthropic API usage response.
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

// Package-level aliases for SandboxActivity constants to reduce verbosity
// in sandbox routing call sites throughout the runner package.
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
// buildContainerSpecForSandbox constructs a ContainerSpec for the given sandbox
// with all workspace mounts, labels, board context, and agent command configured.
func (r *Runner) buildContainerSpecForSandbox(
	containerName, taskID, prompt, sessionID string,
	worktreeOverrides map[string]string,
	boardDir string,
	siblingMounts map[string]map[string]string,
	modelOverride string,
	sb sandbox.Type,
) sandbox.ContainerSpec {
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

	// Host mode takes a separate path: no mounts, host paths verbatim,
	// context surfaced via env vars instead of /workspace/.tasks/ mounts.
	if r.HostMode() {
		return r.buildHostSpec(spec, prompt, model, sessionID, sb, worktreeOverrides, boardDir, siblingMounts)
	}

	// Mount workspaces, substituting per-task worktree paths where available.
	// Read under storeMu to avoid racing with applyWorkspaceSnapshot.
	workspaces := r.currentWorkspaces()
	var basenames []string
	if len(workspaces) > 0 {
		for _, ws := range workspaces {
			ws = strings.TrimSpace(ws)
			if ws == "" {
				continue
			}
			hostPath := ws
			if wt, ok := worktreeOverrides[ws]; ok {
				hostPath = wt
			}
			basename := sanitizeBasename(ws)
			basenames = append(basenames, basename)
			spec.Volumes = append(spec.Volumes, sandbox.VolumeMount{
				Host:      hostPath,
				Container: "/workspace/" + basename,
				Options:   mountOpts("z"),
			})

			// Git worktrees have a .git file (not directory) that references
			// the main repo's .git/worktrees/<name>/ using an absolute host
			// path. Mount the main repo's .git directory at the same host
			// path inside the container so git operations work correctly.
			// On macOS, /var is a symlink to /private/var, so git may store
			// the resolved path in the worktree's .git file. Mount at both
			// the original and resolved paths to handle this.
			if _, isWorktree := worktreeOverrides[ws]; isWorktree {
				gitDir := filepath.Join(ws, ".git")
				if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
					spec.Volumes = append(spec.Volumes, sandbox.VolumeMount{
						Host:      gitDir,
						Container: gitDir,
						Options:   mountOpts("z"),
					})
					// Also mount at the symlink-resolved path if it differs
					// (e.g. macOS /var -> /private/var).
					if resolved, err := filepath.EvalSymlinks(gitDir); err == nil && resolved != gitDir {
						spec.Volumes = append(spec.Volumes, sandbox.VolumeMount{
							Host:      gitDir,
							Container: resolved,
							Options:   mountOpts("z"),
						})
					}
				}
			}
		}
	}

	// Mount workspace-level instructions file based on sandbox convention:
	// - Claude sandbox expects CLAUDE.md
	// - Codex sandbox expects AGENTS.md
	// For single workspace, mount inside the repo directory so that the
	// agent stays anchored to the repo root rather than /workspace/.
	spec.Volumes = r.appendInstructionsMount(spec.Volumes, sb, basenames)

	// Board context: mount board.json read-only at /workspace/.tasks/.
	if boardDir != "" {
		spec.Volumes = append(spec.Volumes, sandbox.VolumeMount{
			Host:      boardDir,
			Container: "/workspace/.tasks",
			Options:   mountOpts("z", "ro"),
		})
	}

	// Sibling worktrees: mount each eligible sibling's worktrees read-only.
	// Sort by shortID then by repoPath for deterministic output.
	shortIDs := make([]string, 0, len(siblingMounts))
	for shortID := range siblingMounts {
		shortIDs = append(shortIDs, shortID)
	}
	slices.Sort(shortIDs)
	for _, shortID := range shortIDs {
		repos := siblingMounts[shortID]
		repoPaths := make([]string, 0, len(repos))
		for repoPath := range repos {
			repoPaths = append(repoPaths, repoPath)
		}
		slices.Sort(repoPaths)
		for _, repoPath := range repoPaths {
			wtPath := repos[repoPath]
			basename := sanitizeBasename(repoPath)
			containerPath := "/workspace/.tasks/worktrees/" + shortID + "/" + basename
			spec.Volumes = append(spec.Volumes, sandbox.VolumeMount{
				Host:      wtPath,
				Container: containerPath,
				Options:   mountOpts("z", "ro"),
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
	return spec
}

// buildHostSpec fills in the fields of a base ContainerSpec for host-mode
// execution. It deliberately does NOT append to spec.Volumes — host mode has
// no mounts. Instead, the composed workspace instructions, the board
// manifest, and the sibling-worktree table are surfaced to the agent via
// WALLFACER_* environment variables the HostBackend and agents can consult.
//
// CWD:
//   - Single workspace: CWD is the host path of that workspace (worktree
//     override applied when available).
//   - Multi-workspace: pick the first; the agent can reach the others via
//     the manifest file referenced by WALLFACER_SIBLING_WORKTREES_JSON. No
//     pseudo-root like /workspace exists on the host.
//
// The returned spec has Entrypoint cleared (the host binary is the CLI
// itself — no dispatcher entrypoint to invoke).
func (r *Runner) buildHostSpec(
	spec sandbox.ContainerSpec,
	prompt, model, sessionID string,
	_ sandbox.Type,
	worktreeOverrides map[string]string,
	boardDir string,
	siblingMounts map[string]map[string]string,
) sandbox.ContainerSpec {
	// The agents-image entrypoint is meaningless on the host — we invoke
	// the CLI binary directly.
	spec.Entrypoint = ""
	// Drop any base-spec mounts that describe container-only artifacts.
	// HostBackend ignores spec.Volumes anyway, but dropping them here keeps
	// tests that snapshot the spec readable.
	spec.Volumes = nil

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

	// Surface instructions / board / siblings via env vars. HostBackend reads
	// WALLFACER_INSTRUCTIONS_PATH to decide between --append-system-prompt
	// and prompt-prepend fallback.
	if instr := r.currentInstructionsPath(); instr != "" {
		if _, err := os.Stat(instr); err == nil {
			spec.Env["WALLFACER_INSTRUCTIONS_PATH"] = instr
		}
	}
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

// instructionsFilenameForSandbox returns the container-side filename for
// workspace-level instructions. Claude expects CLAUDE.md; Codex expects AGENTS.md.
func instructionsFilenameForSandbox(sb sandbox.Type) string {
	if sb == sandbox.Codex {
		return prompts.CodexInstructionsFilename
	}
	return prompts.ClaudeInstructionsFilename
}

// appendInstructionsMount adds the workspace-level instructions file as a
// read-only bind mount (CLAUDE.md for claude, AGENTS.md for codex).
// When there is exactly one workspace, the file is mounted inside that
// workspace directory (e.g. /workspace/<repo>/CLAUDE.md) so the agent
// stays anchored to the repo root. For multiple workspaces the file is
// mounted at /workspace/ so it is accessible from the common root.
// It is a no-op when instructionsPath is empty or does not exist on the host.
func (r *Runner) appendInstructionsMount(volumes []sandbox.VolumeMount, sb sandbox.Type, basenames []string) []sandbox.VolumeMount {
	instrPath := r.currentInstructionsPath()
	if instrPath == "" {
		return volumes
	}
	if _, err := os.Stat(instrPath); err != nil {
		return volumes
	}
	filename := instructionsFilenameForSandbox(sb)
	containerPath := "/workspace/" + filename
	if len(basenames) == 1 {
		containerPath = "/workspace/" + basenames[0] + "/" + filename
	}
	return append(volumes, sandbox.VolumeMount{
		Host:      instrPath,
		Container: containerPath,
		Options:   mountOpts("z", "ro"),
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

// appendCodexAuthMount adds the host Codex auth.json file as a read-only
// bind mount when the sandbox is Codex and the file exists. No-op for
// other sandboxes.
//
// Only the file is mounted (not the entire ~/.codex directory): codex
// 0.120+ writes config.toml and session state into $HOME/.codex at
// startup, so the directory itself must remain writable inside the
// container. Mounting the whole dir read-only would break the CLI;
// mounting it read-write would let the container clobber host state.
func (r *Runner) appendCodexAuthMount(volumes []sandbox.VolumeMount, sb sandbox.Type) []sandbox.VolumeMount {
	if sb != sandbox.Codex {
		return volumes
	}
	if hostDir := r.hostCodexAuthPath(); hostDir != "" {
		volumes = append(volumes, sandbox.VolumeMount{
			Host:      filepath.Join(hostDir, "auth.json"),
			Container: "/home/agent/.codex/auth.json",
			Options:   mountOpts("z", "ro"),
		})
	}
	return volumes
}

// sandboxEntrypoint is the in-image dispatcher script. The sandbox-agents
// image installs a single entrypoint that reads WALLFACER_AGENT to decide
// whether to launch claude-agent.sh or codex-agent.sh; both classic and
// worker exec invocations point at the same path.
const sandboxEntrypoint = "/usr/local/bin/entrypoint.sh"

// buildBaseContainerSpec creates a ContainerSpec pre-populated with the
// configuration shared across all sub-agent invocations:
//   - Runtime, Name, and the unified sandbox-agents image
//   - EnvFile (when configured)
//   - WALLFACER_AGENT environment variable (claude or codex) so the image
//     entrypoint dispatches to the correct CLI
//   - CLAUDE_CODE_MODEL environment variable (when model is non-empty)
//   - claude-config named volume for agent configuration persistence
//   - Codex auth.json bind-mount (when sandbox=="codex" and the file exists)
//
// Callers set Labels, additional Volumes (workspace directories, instructions
// file, board context), WorkDir, and Cmd for their specific needs.
func (r *Runner) buildBaseContainerSpec(containerName, model string, sb sandbox.Type) sandbox.ContainerSpec {
	spec := sandbox.ContainerSpec{
		Runtime: r.command,
		Name:    containerName,
		Image:   strings.TrimSpace(r.sandboxImage),
	}
	if r.envFile != "" {
		spec.EnvFile = r.envFile
	}
	spec.Env = map[string]string{"WALLFACER_AGENT": string(sb)}
	if model != "" {
		spec.Env["CLAUDE_CODE_MODEL"] = model
	}
	spec.Volumes = append(spec.Volumes, sandbox.VolumeMount{
		Host:      "claude-config",
		Container: "/home/agent/.claude",
		Named:     true,
	})
	spec.Volumes = r.appendCodexAuthMount(spec.Volumes, sb)
	spec.Volumes = r.appendDependencyCacheVolumes(spec.Volumes)
	spec.Entrypoint = sandboxEntrypoint
	spec.Network = r.resolvedContainerNetwork()
	spec.CPUs = r.resolvedContainerCPUs()
	spec.Memory = r.resolvedContainerMemory()
	return spec
}

// dependencyCacheVolumes are the common dependency cache directories
// mounted as named volumes so warm caches persist across container lifetimes.
// Paths target the unified sandbox-agents image's `agent` user home.
var dependencyCacheVolumes = []struct {
	suffix    string // volume name suffix (e.g. "npm")
	container string // container path
}{
	{"npm", "/home/agent/.npm"},
	{"pip", "/home/agent/.cache/pip"},
	{"cargo", "/home/agent/.cargo/registry"},
	{"go-build", "/home/agent/.cache/go-build"},
}

// appendDependencyCacheVolumes adds named volumes for dependency caches when
// WALLFACER_DEPENDENCY_CACHES is enabled. Volume names include the workspace
// key so different workspace groups don't share caches.
func (r *Runner) appendDependencyCacheVolumes(volumes []sandbox.VolumeMount) []sandbox.VolumeMount {
	if r.envFile == "" {
		return volumes
	}
	cfg, err := envconfig.Parse(r.envFile)
	if err != nil || !cfg.DependencyCaches {
		return volumes
	}
	wsKey := r.currentWSKey()
	if wsKey == "" {
		wsKey = "default"
	}
	for _, cache := range dependencyCacheVolumes {
		volumes = append(volumes, sandbox.VolumeMount{
			Host:      "wallfacer-cache-" + cache.suffix + "-" + wsKey,
			Container: cache.container,
			Named:     true,
		})
	}
	return volumes
}

// sandboxForTask returns the resolved sandbox type for the task's implementation activity.
// Shorthand for sandboxForTaskActivity(task, activityImplementation).
func (r *Runner) sandboxForTask(task *store.Task) sandbox.Type {
	return r.sandboxForTaskActivity(task, activityImplementation)
}

// sandboxForTaskActivity resolves the sandbox type for a given task and activity.
// Resolution priority: per-task per-activity override → per-task sandbox → env-file
// per-activity setting → env-file default sandbox → Claude (hardcoded fallback).
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

// sandboxFromEnvForActivity reads the env-file sandbox routing for a specific activity.
// Falls back to cfg.DefaultSandbox when no activity-specific override is set.
// Returns "" when the env file is absent or unparseable.
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

// modelFromEnv reads CLAUDE_DEFAULT_MODEL from the env file.
// Returns an empty string when the file is absent or the key is unset.
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
	// Initially register by name; upgraded to handle after Launch succeeds.
	r.taskContainers.Set(taskID, containerName)
	defer r.taskContainers.Delete(taskID)

	sb := sandbox.Claude
	if task, err := r.taskStore(taskID).GetTask(r.shutdownCtx, taskID); err == nil {
		sb = r.sandboxForTaskActivity(task, activity)
	} else {
		logger.Runner.Warn("runContainer: get task", "task", taskID, "error", err)
	}

	// runWithSandbox encapsulates the full launch-read-parse cycle for a
	// single sandbox type. It is called once with the configured sandbox,
	// and possibly a second time with Codex if a token/rate limit is detected
	// (claude→codex fallback).
	runWithSandbox := func(selectedSandbox sandbox.Type) (*agentOutput, []byte, []byte, error) {
		// Refuse to launch if the container runtime is known-unavailable.
		if !r.containerCB.Allow() {
			return nil, nil, nil, fmt.Errorf("container circuit breaker open: container runtime may be unavailable")
		}

		spec := r.buildContainerSpecForSandbox(containerName, taskID.String(), prompt, sessionID, worktreeOverrides, boardDir, siblingMounts, modelOverride, selectedSandbox)
		if spec.Labels != nil {
			spec.Labels["wallfacer.task.activity"] = string(activity)
		}

		logger.Runner.Debug("exec", "cmd", spec.Runtime, "name", spec.Name, "sandbox", selectedSandbox, "workdir", spec.WorkDir, "volumes", len(spec.Volumes))
		_ = r.taskStore(taskID).InsertEvent(ctx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "container_run", Label: string(activity)})

		handle, launchErr := r.backend.Launch(ctx, spec)
		if launchErr != nil {
			_ = r.taskStore(taskID).InsertEvent(ctx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "container_run", Label: string(activity)})
			r.containerCB.RecordFailure()
			return nil, nil, nil, fmt.Errorf("launch container: %w", launchErr)
		}
		// Upgrade registry entry with the handle so kill goes through it.
		r.taskContainers.SetHandle(taskID, handle, nil)

		// Set up a live log buffer so the streaming handler can serve
		// output while the container is still running. Both stdout and
		// stderr are tee'd into the buffer and read concurrently.
		ll := newLiveLog()
		r.liveLogs.Store(taskID, ll)
		defer func() {
			ll.Close()
			r.liveLogs.Delete(taskID)
		}()

		var rawStdout, rawStderr []byte
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			rawStdout, _ = io.ReadAll(io.TeeReader(handle.Stdout(), ll))
		}()
		go func() {
			defer wg.Done()
			rawStderr, _ = io.ReadAll(io.TeeReader(handle.Stderr(), ll))
		}()
		wg.Wait()
		exitCode, waitErr := handle.Wait()
		_ = r.taskStore(taskID).InsertEvent(ctx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "container_run", Label: string(activity)})

		// Detect container runtime failures (exit code 125 = engine error).
		if exitCode == 125 && ctx.Err() == nil {
			r.containerCB.RecordFailure()
		}

		// If the context was cancelled or timed out, kill the container explicitly
		// and return the context error rather than parsing potentially incomplete output.
		if ctx.Err() != nil {
			_ = handle.Kill()
			return nil, rawStdout, rawStderr, fmt.Errorf("container terminated: %w", ctx.Err())
		}

		raw := strings.TrimSpace(string(rawStdout))
		if raw == "" {
			if waitErr != nil {
				return nil, rawStdout, rawStderr, fmt.Errorf("exec container: %w", waitErr)
			}
			if exitCode != 0 {
				return nil, rawStdout, rawStderr,
					fmt.Errorf("container exited with code %d: stderr=%s", exitCode, string(rawStderr))
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
			if waitErr != nil {
				return nil, rawStdout, rawStderr, fmt.Errorf("exec container: %w", waitErr)
			}
			if exitCode != 0 {
				return nil, rawStdout, rawStderr,
					fmt.Errorf("container exited with code %d: stderr=%s stdout=%s",
						exitCode, string(rawStderr), truncate(raw, 500))
			}
			return nil, rawStdout, rawStderr,
				fmt.Errorf("parse output: %w (raw: %s)", parseErr, truncate(raw, 200))
		}

		// The agent may exit non-zero even when it produces a valid result.
		if exitCode != 0 {
			logger.Runner.Warn("container exited non-zero but produced valid output",
				"task", taskID, "code", exitCode, "sandbox", selectedSandbox)
		}

		// Container runtime is healthy: close the circuit (or keep it closed).
		r.containerCB.RecordSuccess()
		output.ActualSandbox = selectedSandbox
		return output, rawStdout, rawStderr, nil
	}

	// Primary attempt with the configured sandbox, followed by automatic
	// claude→codex fallback on token/rate limit errors (checked twice:
	// once for launch/exec errors, once for is_error in the parsed output).
	output, rawStdout, rawStderr, err := runWithSandbox(sb)
	if err != nil {
		if sb == sandbox.Claude && isLikelyTokenLimitError(err.Error(), string(rawStderr)) {
			logger.Runner.Warn("claude sandbox token limit hit; retrying with codex",
				"task", taskID, "activity", activity)
			_ = r.taskStore(taskID).InsertEvent(ctx, taskID, store.EventTypeSystem, map[string]string{

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
		_ = r.taskStore(taskID).InsertEvent(ctx, taskID, store.EventTypeSystem, map[string]string{

			"result": "Sandbox fallback: claude → codex (token/rate limit in output)",
		})
		return runWithSandbox(sandbox.Codex)
	}

	return output, rawStdout, rawStderr, nil
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
