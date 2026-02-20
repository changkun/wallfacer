package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Runner struct {
	store        *Store
	command      string
	sandboxImage string
	envFile      string
	workspaces   string
}

type RunnerConfig struct {
	Command      string
	SandboxImage string
	EnvFile      string
	Workspaces   string
}

func NewRunner(store *Store, cfg RunnerConfig) *Runner {
	return &Runner{
		store:        store,
		command:      cfg.Command,
		sandboxImage: cfg.SandboxImage,
		envFile:      cfg.EnvFile,
		workspaces:   cfg.Workspaces,
	}
}

type claudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

type claudeOutput struct {
	Result       string     `json:"result"`
	SessionID    string     `json:"session_id"`
	StopReason   string     `json:"stop_reason"`
	IsError      bool       `json:"is_error"`
	TotalCostUSD float64    `json:"total_cost_usd"`
	Usage        claudeUsage `json:"usage"`
}

func (r *Runner) Command() string {
	return r.command
}

func (r *Runner) Workspaces() []string {
	if r.workspaces == "" {
		return nil
	}
	return strings.Fields(r.workspaces)
}

func (r *Runner) Run(taskID uuid.UUID, prompt, sessionID string) {
	bgCtx := context.Background()
	resumedFromWaiting := sessionID != ""

	task, err := r.store.GetTask(bgCtx, taskID)
	if err != nil {
		log.Printf("runner: get task %s: %v", taskID, err)
		return
	}

	// Apply per-task total timeout across all turns.
	timeout := time.Duration(task.Timeout) * time.Minute
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(bgCtx, timeout)
	defer cancel()

	turns := task.Turns

	for {
		turns++
		log.Printf("runner: task %s turn %d (session=%s, timeout=%s)", taskID, turns, sessionID, timeout)

		output, rawStdout, rawStderr, err := r.runContainer(ctx, taskID, prompt, sessionID)
		if saveErr := r.store.SaveTurnOutput(taskID, turns, rawStdout, rawStderr); saveErr != nil {
			log.Printf("runner: task %s save turn %d output: %v", taskID, turns, saveErr)
		}
		if err != nil {
			log.Printf("runner: task %s container error: %v", taskID, err)
			r.store.UpdateTaskStatus(bgCtx, taskID, "failed")
			r.store.UpdateTaskResult(bgCtx, taskID, err.Error(), sessionID, "", turns)
			r.store.InsertEvent(bgCtx, taskID, "error", map[string]string{"error": err.Error()})
			r.store.InsertEvent(bgCtx, taskID, "state_change", map[string]string{
				"from": "in_progress", "to": "failed",
			})
			return
		}

		r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
			"result":      output.Result,
			"stop_reason": output.StopReason,
			"session_id":  output.SessionID,
		})

		if output.SessionID != "" {
			sessionID = output.SessionID
		}
		r.store.UpdateTaskResult(bgCtx, taskID, output.Result, sessionID, output.StopReason, turns)
		r.store.AccumulateTaskUsage(bgCtx, taskID, TaskUsage{
			InputTokens:          output.Usage.InputTokens,
			OutputTokens:         output.Usage.OutputTokens,
			CacheReadInputTokens: output.Usage.CacheReadInputTokens,
			CacheCreationTokens:  output.Usage.CacheCreationInputTokens,
			CostUSD:              output.TotalCostUSD,
		})

		if output.IsError {
			r.store.UpdateTaskStatus(bgCtx, taskID, "failed")
			r.store.InsertEvent(bgCtx, taskID, "state_change", map[string]string{
				"from": "in_progress", "to": "failed",
			})
			return
		}

		switch output.StopReason {
		case "end_turn":
			r.store.UpdateTaskStatus(bgCtx, taskID, "done")
			r.store.InsertEvent(bgCtx, taskID, "state_change", map[string]string{
				"from": "in_progress", "to": "done",
			})

			// Auto-commit after feedback-resumed tasks complete.
			if resumedFromWaiting && sessionID != "" {
				r.commit(ctx, taskID, sessionID, turns)
			}
			return

		case "max_tokens", "pause_turn":
			log.Printf("runner: task %s auto-continuing (stop_reason=%s)", taskID, output.StopReason)
			prompt = ""
			continue

		default:
			// Empty or unknown stop_reason — waiting for user feedback
			r.store.UpdateTaskStatus(bgCtx, taskID, "waiting")
			r.store.InsertEvent(bgCtx, taskID, "state_change", map[string]string{
				"from": "in_progress", "to": "waiting",
			})
			return
		}
	}
}

// Commit creates its own timeout context and runs an auto-commit turn for a task.
func (r *Runner) Commit(taskID uuid.UUID, sessionID string) {
	task, err := r.store.GetTask(context.Background(), taskID)
	if err != nil {
		log.Printf("runner: commit get task %s: %v", taskID, err)
		return
	}
	timeout := time.Duration(task.Timeout) * time.Minute
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	r.commit(ctx, taskID, sessionID, task.Turns)
}

// workspacePaths returns the container-side paths for mounted workspaces.
func (r *Runner) workspacePaths() []string {
	var paths []string
	if r.workspaces == "" {
		return paths
	}
	for _, ws := range strings.Fields(r.workspaces) {
		ws = strings.TrimSpace(ws)
		if ws == "" {
			continue
		}
		parts := strings.Split(ws, "/")
		basename := parts[len(parts)-1]
		if basename == "" && len(parts) > 1 {
			basename = parts[len(parts)-2]
		}
		paths = append(paths, "/workspace/"+basename)
	}
	return paths
}

// commit runs an additional container turn to stage and commit changes.
func (r *Runner) commit(ctx context.Context, taskID uuid.UUID, sessionID string, turns int) {
	bgCtx := context.Background()
	log.Printf("runner: task %s auto-commit (session=%s)", taskID, sessionID)

	r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
		"result": "Auto-running commit...",
	})

	dirs := r.workspacePaths()
	var prompt string
	if len(dirs) > 0 {
		prompt = fmt.Sprintf(
			"Commit all changes. The workspace repositories are at: %s. "+
				"For each directory, cd into it, run `git status`, and if there are "+
				"uncommitted changes, stage them with `git add -A` and create a commit "+
				"with a descriptive message summarizing the changes. "+
				"Report what you committed.",
			strings.Join(dirs, ", "))
	} else {
		prompt = "Commit all changes. Check `git status` in each subdirectory " +
			"of /workspace. If there are uncommitted changes, stage them with `git add -A` " +
			"and create a commit with a descriptive message summarizing the changes. " +
			"Report what you committed."
	}

	turns++
	output, rawStdout, rawStderr, err := r.runContainer(ctx, taskID, prompt, sessionID)
	if saveErr := r.store.SaveTurnOutput(taskID, turns, rawStdout, rawStderr); saveErr != nil {
		log.Printf("runner: task %s save commit turn %d output: %v", taskID, turns, saveErr)
	}
	if err != nil {
		log.Printf("runner: task %s commit error: %v", taskID, err)
		r.store.InsertEvent(bgCtx, taskID, "error", map[string]string{
			"error": "commit failed: " + err.Error(),
		})
		return
	}

	log.Printf("runner: task %s commit result: %s", taskID, truncate(output.Result, 500))

	r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
		"result":      output.Result,
		"stop_reason": output.StopReason,
		"session_id":  output.SessionID,
	})

	// Keep the original task session ID — this is a throwaway session.
	r.store.UpdateTaskResult(bgCtx, taskID, output.Result, sessionID, output.StopReason, turns)
	r.store.AccumulateTaskUsage(bgCtx, taskID, TaskUsage{
		InputTokens:          output.Usage.InputTokens,
		OutputTokens:         output.Usage.OutputTokens,
		CacheReadInputTokens: output.Usage.CacheReadInputTokens,
		CacheCreationTokens:  output.Usage.CacheCreationInputTokens,
		CostUSD:              output.TotalCostUSD,
	})
	log.Printf("runner: task %s commit completed", taskID)
}

func (r *Runner) runContainer(ctx context.Context, taskID uuid.UUID, prompt, sessionID string) (*claudeOutput, []byte, []byte, error) {
	containerName := "wallfacer-" + taskID.String()

	// Remove any leftover container from a previous interrupted run.
	exec.Command(r.command, "rm", "-f", containerName).Run()

	args := []string{"run", "--rm", "--network=host", "--name", containerName}

	if r.envFile != "" {
		args = append(args, "--env-file", r.envFile)
	}

	// Mount claude config volume.
	args = append(args, "-v", "claude-config:/home/claude/.claude")

	// Mount workspaces.
	if r.workspaces != "" {
		for _, ws := range strings.Fields(r.workspaces) {
			ws = strings.TrimSpace(ws)
			if ws == "" {
				continue
			}
			parts := strings.Split(ws, "/")
			basename := parts[len(parts)-1]
			if basename == "" && len(parts) > 1 {
				basename = parts[len(parts)-2]
			}
			args = append(args, "-v", ws+":/workspace/"+basename+":z")
		}
	}

	args = append(args, "-w", "/workspace", r.sandboxImage)
	args = append(args, "-p", prompt, "--output-format", "json")
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}

	cmd := exec.CommandContext(ctx, r.command, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	log.Printf("runner: exec %s %s", r.command, strings.Join(args, " "))
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, stdout.Bytes(), stderr.Bytes(), fmt.Errorf("container exited with code %d: stderr=%s stdout=%s", exitErr.ExitCode(), stderr.String(), truncate(stdout.String(), 500))
		}
		return nil, stdout.Bytes(), stderr.Bytes(), fmt.Errorf("exec container: %w", err)
	}

	var output claudeOutput
	raw := strings.TrimSpace(stdout.String())
	if raw == "" {
		return nil, stdout.Bytes(), stderr.Bytes(), fmt.Errorf("empty output from container")
	}
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		return nil, stdout.Bytes(), stderr.Bytes(), fmt.Errorf("parse output: %w (raw: %s)", err, truncate(raw, 200))
	}

	return &output, stdout.Bytes(), stderr.Bytes(), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
