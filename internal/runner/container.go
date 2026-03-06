package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"changkun.de/wallfacer/internal/envconfig"
	"changkun.de/wallfacer/internal/logger"
	"github.com/google/uuid"
)

// claudeUsage mirrors the token-usage object in Claude Code's JSON output.
type claudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// claudeOutput is the top-level result object emitted by Claude Code
// (either as a single JSON blob or as the last line of NDJSON stream-json).
type claudeOutput struct {
	Result       string      `json:"result"`
	SessionID    string      `json:"session_id"`
	StopReason   string      `json:"stop_reason"`
	Subtype      string      `json:"subtype"`
	IsError      bool        `json:"is_error"`
	TotalCostUSD float64     `json:"total_cost_usd"`
	Usage        claudeUsage `json:"usage"`
}

// buildContainerArgs constructs the full argument list for the container run command.
// It is a pure function of runner configuration and the supplied parameters,
// which makes it easy to unit-test without actually launching a container.
//
// boardDir, when non-empty, is a host directory containing board.json that
// will be mounted read-only at /workspace/.tasks/ inside the container.
// siblingMounts maps shortID → (repoPath → worktreePath) for read-only
// sibling worktree mounts under /workspace/.tasks/worktrees/.
func (r *Runner) buildContainerArgs(
	containerName, prompt, sessionID string,
	worktreeOverrides map[string]string,
	boardDir string,
	siblingMounts map[string]map[string]string,
	modelOverride string,
) []string {
	args := []string{"run", "--rm", "--network=host", "--name", containerName}

	if r.envFile != "" {
		args = append(args, "--env-file", r.envFile)
	}

	// Inject CLAUDE_CODE_MODEL so Claude Code's subagent model selection
	// also uses the configured model (not just the --model CLI flag which
	// only affects the main session).
	if m := modelOverride; m != "" {
		args = append(args, "-e", "CLAUDE_CODE_MODEL="+m)
	} else if m := r.modelFromEnv(); m != "" {
		args = append(args, "-e", "CLAUDE_CODE_MODEL="+m)
	}

	// Mount claude config volume.
	args = append(args, "-v", "claude-config:/home/claude/.claude")

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
			args = append(args, "-v", hostPath+":/workspace/"+basename+":z")

			// Git worktrees have a .git file (not directory) that references
			// the main repo's .git/worktrees/<name>/ using an absolute host
			// path. Mount the main repo's .git directory at the same host
			// path inside the container so git operations work correctly.
			if _, isWorktree := worktreeOverrides[ws]; isWorktree {
				gitDir := filepath.Join(ws, ".git")
				if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
					args = append(args, "-v", gitDir+":"+gitDir+":z")
				}
			}
		}
	}

	// Mount workspace-level CLAUDE.md so Claude Code picks it up automatically.
	// Claude Code searches for CLAUDE.md at the project root (where .git is)
	// and at ~/.claude/, but NOT in parent directories above the project root.
	// For single-workspace tasks, CWD is /workspace/<basename> which IS the
	// project root, so /workspace/CLAUDE.md (the parent) would be invisible.
	// Mount directly into the workspace root instead.
	if r.instructionsPath != "" {
		if _, err := os.Stat(r.instructionsPath); err == nil {
			if len(basenames) == 1 {
				args = append(args, "-v", r.instructionsPath+":/workspace/"+basenames[0]+"/CLAUDE.md:z,ro")
			} else {
				args = append(args, "-v", r.instructionsPath+":/workspace/CLAUDE.md:z,ro")
			}
		}
	}

	// Board context: mount board.json read-only at /workspace/.tasks/.
	if boardDir != "" {
		args = append(args, "-v", boardDir+":/workspace/.tasks:z,ro")
	}

	// Sibling worktrees: mount each eligible sibling's worktrees read-only.
	for shortID, repos := range siblingMounts {
		for repoPath, wtPath := range repos {
			basename := filepath.Base(repoPath)
			containerPath := "/workspace/.tasks/worktrees/" + shortID + "/" + basename
			args = append(args, "-v", wtPath+":"+containerPath+":z,ro")
		}
	}

	// When there is exactly one workspace, set CWD directly into it so
	// Claude operates in the repo directory by default. For multiple
	// workspaces keep CWD at /workspace so all repos are accessible.
	workdir := "/workspace"
	if len(basenames) == 1 {
		workdir = "/workspace/" + basenames[0]
	}
	args = append(args, "-w", workdir, r.sandboxImage)
	args = append(args, "-p", prompt, "--verbose", "--output-format", "stream-json")
	// Per-task model takes priority; fall back to the env-configured default.
	if modelOverride != "" {
		args = append(args, "--model", modelOverride)
	} else if model := r.modelFromEnv(); model != "" {
		args = append(args, "--model", model)
	}
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}

	return args
}

// modelFromEnv reads WALLFACER_DEFAULT_MODEL from the env file (if configured).
// Returns an empty string when the file cannot be read or the key is absent.
func (r *Runner) modelFromEnv() string {
	if r.envFile == "" {
		return ""
	}
	cfg, err := envconfig.Parse(r.envFile)
	if err != nil {
		return ""
	}
	return cfg.DefaultModel
}

// titleModelFromEnv reads WALLFACER_TITLE_MODEL from the env file,
// falling back to WALLFACER_DEFAULT_MODEL if the title model is not set.
func (r *Runner) titleModelFromEnv() string {
	if r.envFile == "" {
		return ""
	}
	cfg, err := envconfig.Parse(r.envFile)
	if err != nil {
		return ""
	}
	if cfg.TitleModel != "" {
		return cfg.TitleModel
	}
	return cfg.DefaultModel
}

// runContainer executes a Claude Code container and parses its NDJSON output.
// Returns (output, rawStdout, rawStderr, error).
func (r *Runner) runContainer(
	ctx context.Context,
	taskID uuid.UUID,
	prompt, sessionID string,
	worktreeOverrides map[string]string,
	boardDir string,
	siblingMounts map[string]map[string]string,
	modelOverride string,
) (*claudeOutput, []byte, []byte, error) {
	containerName := "wallfacer-" + taskID.String()

	// Remove any leftover container from a previous interrupted run.
	exec.Command(r.command, "rm", "-f", containerName).Run()

	args := r.buildContainerArgs(containerName, prompt, sessionID, worktreeOverrides, boardDir, siblingMounts, modelOverride)

	cmd := exec.CommandContext(ctx, r.command, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	logger.Runner.Debug("exec", "cmd", r.command, "args", strings.Join(args, " "))
	runErr := cmd.Run()

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

	// Claude Code may exit non-zero even when it produces a valid result.
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			logger.Runner.Warn("container exited non-zero but produced valid output",
				"task", taskID, "code", exitErr.ExitCode())
		} else {
			logger.Runner.Warn("container error but produced valid output", "task", taskID, "error", runErr)
		}
	}

	return output, stdout.Bytes(), stderr.Bytes(), nil
}

// parseOutput tries to parse raw as a single JSON object first; if that fails
// it scans backwards through NDJSON lines looking for the last valid object.
func parseOutput(raw string) (*claudeOutput, error) {
	var output claudeOutput
	if err := json.Unmarshal([]byte(raw), &output); err == nil {
		return &output, nil
	}
	lines := strings.Split(raw, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		if err := json.Unmarshal([]byte(line), &output); err == nil {
			return &output, nil
		}
	}
	return nil, fmt.Errorf("no valid JSON object found in output")
}

// extractSessionID scans raw NDJSON output for a session_id field.
// Claude Code emits session_id in early stream messages, so it is often
// present even when the container is killed mid-execution (e.g. timeout).
func extractSessionID(raw []byte) string {
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var obj struct {
			SessionID string `json:"session_id"`
		}
		if json.Unmarshal([]byte(line), &obj) == nil && obj.SessionID != "" {
			return obj.SessionID
		}
	}
	return ""
}

// runGit is a helper to run a git command and discard output (best-effort).
func runGit(dir string, args ...string) error {
	return exec.Command("git", append([]string{"-C", dir}, args...)...).Run()
}
