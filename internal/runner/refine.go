package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

const refinementTimeout = 30 * time.Minute

// refinementPromptTemplate wraps the task prompt with instructions that
// direct the sandbox agent to produce a spec without making code changes.
const refinementPromptTemplate = `You are a task specification writer. DO NOT write any code or make any changes to files.

Your goal is to explore the codebase and produce a detailed implementation specification for the following task:

<task>
%s
</task>

Instructions:
1. Explore relevant parts of the codebase to understand context and existing patterns
2. Identify the best implementation approach given what already exists
3. Produce a comprehensive spec using this format:

# Implementation Spec

## Objective
[Clear statement of what needs to be achieved and why]

## Background
[Relevant context from the codebase that informs the approach]

## Implementation Plan
[Numbered list of concrete implementation steps]

## Files to Change
[Specific files and what changes are needed in each]

## Edge Cases & Considerations
[Important things to handle or watch out for]

Be specific and concrete. The spec should be detailed enough that a developer can implement it without further clarification.

DO NOT implement anything — only produce the spec.`

// RunRefinement runs the sandbox agent in read-only mode to produce a
// detailed implementation spec for the task's current prompt. The task
// stays in backlog; only CurrentRefinement is updated to track state.
// userInstructions is an optional hint from the user that narrows the
// agent's focus (e.g. "keep backward compatibility").
func (r *Runner) RunRefinement(taskID uuid.UUID, userInstructions string) {
	bgCtx := context.Background()
	ctx, cancel := context.WithTimeout(bgCtx, refinementTimeout)
	defer cancel()

	task, err := r.store.GetTask(bgCtx, taskID)
	if err != nil {
		logger.Runner.Error("refinement: get task", "task", taskID, "error", err)
		return
	}

	prompt := fmt.Sprintf(refinementPromptTemplate, task.Prompt)
	if strings.TrimSpace(userInstructions) != "" {
		prompt += "\n\nAdditional focus from the user:\n<user_instructions>\n" + strings.TrimSpace(userInstructions) + "\n</user_instructions>"
	}

	output, _, _, err := r.runRefinementContainer(ctx, taskID, prompt, "", r.sandboxForTaskActivity(task, activityRefinement))
	if err != nil {
		logger.Runner.Error("refinement container error", "task", taskID, "error", err)

		// Don't overwrite a cleared job (task may have been reset).
		cur, getErr := r.store.GetTask(bgCtx, taskID)
		if getErr != nil || cur.CurrentRefinement == nil {
			return
		}
		cur.CurrentRefinement.Status = "failed"
		cur.CurrentRefinement.Error = err.Error()
		r.store.UpdateRefinementJob(bgCtx, taskID, cur.CurrentRefinement)
		return
	}

	cur, getErr := r.store.GetTask(bgCtx, taskID)
	if getErr != nil || cur.CurrentRefinement == nil {
		return
	}
	cur.CurrentRefinement.Status = "done"
	cur.CurrentRefinement.Result = cleanRefinementResult(output.Result)
	r.store.UpdateRefinementJob(bgCtx, taskID, cur.CurrentRefinement)

	logger.Runner.Info("refinement complete", "task", taskID)
}

// buildRefinementContainerArgs builds container args for a read-only refinement
// run. Workspaces are mounted read-only; no worktrees, board context, or sibling
// mounts are used since the agent should only read, not commit.
func (r *Runner) buildRefinementContainerArgs(containerName, taskID, prompt, modelOverride, sandbox string) []string {
	model := modelOverride
	if model == "" {
		model = r.modelFromEnvForSandbox(sandbox)
	}

	spec := ContainerSpec{
		Runtime: r.command,
		Name:    containerName,
		Image:   r.sandboxImage,
	}

	if taskID != "" {
		spec.Labels = map[string]string{
			"wallfacer.task.id":    taskID,
			"wallfacer.task.refine": "true",
		}
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

	var basenames []string
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
			basenames = append(basenames, basename)
			// Mount read-only: refinement should inspect, not modify.
			spec.Volumes = append(spec.Volumes, VolumeMount{
				Host:      ws,
				Container: "/workspace/" + basename,
				Options:   "z,ro",
			})
		}
	}

	if r.instructionsPath != "" {
		if _, err := os.Stat(r.instructionsPath); err == nil {
			spec.Volumes = append(spec.Volumes, VolumeMount{
				Host:      r.instructionsPath,
				Container: "/workspace/CLAUDE.md",
				Options:   "z,ro",
			})
		}
	}

	workdir := "/workspace"
	if len(basenames) == 1 {
		workdir = "/workspace/" + basenames[0]
	}
	spec.WorkDir = workdir

	spec.Cmd = []string{"-p", prompt, "--verbose", "--output-format", "stream-json"}
	if model != "" {
		spec.Cmd = append(spec.Cmd, "--model", model)
	}

	return spec.Build()
}

// runRefinementContainer executes a refinement container and parses its output.
// The container name is tracked in refineContainerNames so StreamRefineLogs can
// attach to it for live log streaming.
func (r *Runner) runRefinementContainer(
	ctx context.Context,
	taskID uuid.UUID,
	prompt, modelOverride, sandbox string,
) (*agentOutput, []byte, []byte, error) {
	slug := slugifyPrompt(prompt, 20)
	containerName := "wallfacer-refine-" + slug + "-" + taskID.String()[:8]

	r.refineContainers.Set(taskID, containerName)
	defer r.refineContainers.Delete(taskID)

	exec.Command(r.command, "rm", "-f", containerName).Run()

	args := r.buildRefinementContainerArgs(containerName, taskID.String(), prompt, modelOverride, sandbox)

	cmd := exec.CommandContext(ctx, r.command, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	logger.Runner.Debug("refine exec", "cmd", r.command, "args", strings.Join(args, " "))
	runErr := cmd.Run()

	if ctx.Err() != nil {
		exec.Command(r.command, "kill", containerName).Run()
		exec.Command(r.command, "rm", "-f", containerName).Run()
		return nil, stdout.Bytes(), stderr.Bytes(), fmt.Errorf("refinement container terminated: %w", ctx.Err())
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

	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			logger.Runner.Warn("refinement container exited non-zero but produced valid output",
				"task", taskID, "code", exitErr.ExitCode())
		}
	}

	// Accumulate usage attributed to refinement sub-agent.
	if output.Usage.InputTokens > 0 || output.Usage.OutputTokens > 0 {
		r.store.AccumulateSubAgentUsage(context.Background(), taskID, "refinement", store.TaskUsage{
			InputTokens:          output.Usage.InputTokens,
			OutputTokens:         output.Usage.OutputTokens,
			CacheReadInputTokens: output.Usage.CacheReadInputTokens,
			CacheCreationTokens:  output.Usage.CacheCreationInputTokens,
			CostUSD:              output.TotalCostUSD,
		})
	}

	return output, stdout.Bytes(), stderr.Bytes(), nil
}

// cleanRefinementResult strips any agent preamble (internal monologue,
// separator lines) that appears before the actual spec content.
// It looks for the first top-level markdown heading ("# ") and returns
// everything from that point; if none is found, the original text is returned.
func cleanRefinementResult(result string) string {
	// Check if the result starts directly with a heading.
	if strings.HasPrefix(result, "# ") {
		return result
	}
	// Find the first occurrence of a top-level heading on its own line.
	if idx := strings.Index(result, "\n# "); idx != -1 {
		return strings.TrimSpace(result[idx:])
	}
	return result
}
