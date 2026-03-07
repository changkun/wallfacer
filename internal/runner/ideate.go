package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/logger"
)

const ideationTimeout = 10 * time.Minute

// ideationPromptTemplate instructs the agent to explore the workspace and
// propose exactly 3 actionable improvement ideas as a JSON array.
const ideationPromptTemplate = `You are a software development advisor reviewing the repositories in /workspace/. Your task is to propose exactly 3 high-impact improvements.

First, explore the workspace to understand the project:
- Read README files, CLAUDE.md, go.mod, package.json, or similar project files
- List and scan the main source directories
- Review recent git history if available (git log --oneline -20)

Based on your exploration, identify 3 improvements that would genuinely benefit the project. Consider:
- Bugs, edge cases, or missing error handling observed in the code
- Missing features that users of this project would find valuable
- Performance bottlenecks or scalability concerns
- Code quality, maintainability, or test coverage gaps
- Developer experience improvements
- Security concerns

For each idea, write a detailed prompt that an AI coding agent could execute to implement it.

Output ONLY a JSON array with exactly 3 objects. No preamble, no explanation, no markdown — just the JSON array:
[
  {
    "title": "2-5 word title",
    "prompt": "Detailed implementation prompt for an AI agent. Reference specific files, functions, and patterns from the codebase. Be concrete and actionable."
  },
  {
    "title": "...",
    "prompt": "..."
  },
  {
    "title": "...",
    "prompt": "..."
  }
]`

// IdeateResult holds a single idea proposed by the brainstorm agent.
type IdeateResult struct {
	Title  string `json:"title"`
	Prompt string `json:"prompt"`
}

// RunIdeation runs a lightweight read-only container to analyse the workspaces
// and returns up to 3 proposed task ideas. The caller is responsible for
// creating backlog tasks from the results.
func (r *Runner) RunIdeation(ctx context.Context) ([]IdeateResult, error) {
	containerName := fmt.Sprintf("wallfacer-ideate-%d", time.Now().UnixNano()/1e6)

	r.ideateContainerName.Store("current", containerName)
	defer r.ideateContainerName.Delete("current")

	exec.Command(r.command, "rm", "-f", containerName).Run()

	args := r.buildIdeationContainerArgs(containerName, ideationPromptTemplate)

	cmd := exec.CommandContext(ctx, r.command, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	logger.Runner.Debug("ideate exec", "cmd", r.command, "args", strings.Join(args, " "))
	runErr := cmd.Run()

	if ctx.Err() != nil {
		exec.Command(r.command, "kill", containerName).Run()
		exec.Command(r.command, "rm", "-f", containerName).Run()
		return nil, fmt.Errorf("ideation container terminated: %w", ctx.Err())
	}

	raw := strings.TrimSpace(stdout.String())
	if raw == "" {
		if runErr != nil {
			if exitErr, ok := runErr.(*exec.ExitError); ok {
				return nil, fmt.Errorf("container exited %d: stderr=%s", exitErr.ExitCode(), stderr.String())
			}
			return nil, fmt.Errorf("exec container: %w", runErr)
		}
		return nil, fmt.Errorf("empty output from ideation container")
	}

	output, parseErr := parseOutput(raw)
	if parseErr != nil {
		return nil, fmt.Errorf("parse ideation output: %w", parseErr)
	}
	if output == nil || output.Result == "" {
		return nil, fmt.Errorf("no result in ideation output")
	}

	ideas, err := extractIdeas(output.Result)
	if err != nil {
		return nil, fmt.Errorf("extract ideas: %w (result: %s)", err, truncate(output.Result, 300))
	}
	return ideas, nil
}

// buildIdeationContainerArgs builds the container run arguments for the
// ideation agent. Workspaces are mounted read-only; no task label, no
// worktrees, and no board context are used.
func (r *Runner) buildIdeationContainerArgs(containerName, prompt string) []string {
	args := []string{"run", "--rm", "--network=host", "--name", containerName}

	if r.envFile != "" {
		args = append(args, "--env-file", r.envFile)
	}

	if m := r.modelFromEnv(); m != "" {
		args = append(args, "-e", "CLAUDE_CODE_MODEL="+m)
	}

	args = append(args, "-v", "claude-config:/home/claude/.claude")

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
			// Read-only mount: ideation should only read, not modify.
			args = append(args, "-v", ws+":/workspace/"+basename+":z,ro")
		}
	}

	if r.instructionsPath != "" {
		if _, err := os.Stat(r.instructionsPath); err == nil {
			args = append(args, "-v", r.instructionsPath+":/workspace/CLAUDE.md:z,ro")
		}
	}

	workdir := "/workspace"
	if len(basenames) == 1 {
		workdir = "/workspace/" + basenames[0]
	}
	args = append(args, "-w", workdir, r.sandboxImage)
	args = append(args, "-p", prompt, "--verbose", "--output-format", "stream-json")
	if m := r.modelFromEnv(); m != "" {
		args = append(args, "--model", m)
	}

	return args
}

// extractIdeas finds a JSON array in the agent's text output and parses it
// into a slice of IdeateResult. It is tolerant of surrounding prose by
// scanning for the outermost '[' … ']' pair.
func extractIdeas(text string) ([]IdeateResult, error) {
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON array found in agent output")
	}

	var results []IdeateResult
	if err := json.Unmarshal([]byte(text[start:end+1]), &results); err != nil {
		return nil, fmt.Errorf("unmarshal ideas: %w", err)
	}

	// Filter out any malformed entries.
	var valid []IdeateResult
	for _, r := range results {
		if strings.TrimSpace(r.Title) != "" && strings.TrimSpace(r.Prompt) != "" {
			valid = append(valid, r)
		}
	}
	if len(valid) == 0 {
		return nil, fmt.Errorf("no valid ideas in parsed output")
	}
	return valid, nil
}
