package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

const ideationTimeout = 10 * time.Minute

// ideaCategoryPool is the set of distinct improvement domains from which the
// brainstorm agent draws one per idea. Sampling 3 unique categories per run
// ensures each brainstorm covers genuinely different areas of the project.
var ideaCategoryPool = []string{
	"product feature",
	"frontend / UX",
	"backend / API",
	"performance optimization",
	"code quality / refactoring",
	"test coverage",
	"developer experience",
	"security hardening",
	"observability / debugging",
	"infrastructure / ops",
	"data model / storage",
}

// pickCategories returns n unique categories sampled at random from
// ideaCategoryPool using a Fisher-Yates partial shuffle.
func pickCategories(n int) []string {
	pool := make([]string, len(ideaCategoryPool))
	copy(pool, ideaCategoryPool)
	for i := len(pool) - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		pool[i], pool[j] = pool[j], pool[i]
	}
	if n > len(pool) {
		n = len(pool)
	}
	return pool[:n]
}

// buildIdeationPrompt constructs the full ideation prompt by randomly
// assigning 3 distinct categories — one per idea slot — so that every
// brainstorm run surfaces improvements from different areas of the project.
// existingTasks lists tasks currently in backlog, in_progress, or waiting state
// so the agent can avoid proposing duplicates or conflicting ideas.
func buildIdeationPrompt(existingTasks []store.Task) string {
	cats := pickCategories(3)
	var sb strings.Builder
	sb.WriteString(`You are a software development advisor reviewing the repositories in /workspace/. Your task is to propose exactly 3 improvements — each from a different assigned domain.

First, explore the workspace thoroughly:
- Read README files, AGENTS.md (or legacy CLAUDE.md), go.mod, package.json, or similar project manifests
- Scan the main source directories and read key source files to understand current patterns and pain points
- Review recent git history (git log --oneline -20) to see what has changed recently
- Identify concrete opportunities, rough edges, and gaps in the code

`)
	if len(existingTasks) > 0 {
		sb.WriteString("The following tasks are already queued or actively being worked on. Do NOT propose ideas that duplicate or directly conflict with any of them. If a proposed idea is closely related to an existing task, you MUST reference it explicitly in the prompt field with a note such as: \"Note: This is related to the existing task '[title]' (status: [status]).\"\n\nExisting active tasks:\n")
		for i, t := range existingTasks {
			title := t.Title
			if title == "" {
				title = "(untitled)"
			}
			prompt := strings.TrimSpace(t.Prompt)
			if len(prompt) > 120 {
				prompt = prompt[:120] + "..."
			}
			sb.WriteString(fmt.Sprintf("%d. [%s] (status: %s) — %s\n", i+1, title, string(t.Status), prompt))
		}
		sb.WriteString("\n")
	}
	sb.WriteString(`Then propose exactly 3 improvements, one per assigned domain:
`)
	for i, cat := range cats {
		sb.WriteString(fmt.Sprintf("  Idea %d domain: %s\n", i+1, cat))
	}
	sb.WriteString(`
Requirements for each improvement:
- Technically precise: name the specific files, functions, data structures, or API endpoints you observed during exploration — do not stay generic
- Creative and non-obvious: avoid safe, predictable suggestions like "add more tests" or "improve error handling" in isolation; propose something with genuine engineering interest
- Actionable end-to-end: write a prompt detailed enough for an AI coding agent to implement the full change without asking follow-up questions
- Non-duplicating: do not propose ideas that overlap or conflict with the existing active tasks listed above

Output ONLY a JSON array with exactly 3 objects. No preamble, no explanation, no markdown — just the JSON array:
[
  {
    "title": "2-5 word title",
    "category": "assigned domain for idea 1",
    "prompt": "Detailed implementation prompt referencing specific files, functions, and patterns found during exploration."
  },
  {
    "title": "...",
    "category": "assigned domain for idea 2",
    "prompt": "..."
  },
  {
    "title": "...",
    "category": "assigned domain for idea 3",
    "prompt": "..."
  }
]`)
	return sb.String()
}

// IdeateResult holds a single idea proposed by the brainstorm agent.
type IdeateResult struct {
	Title    string `json:"title"`
	Category string `json:"category"`
	Prompt   string `json:"prompt"`
}

// RunIdeation runs a lightweight read-only container to analyse the workspaces
// and returns up to 3 proposed task ideas together with the raw container
// stdout/stderr and the parsed agent output. The caller is responsible for
// creating backlog tasks from the results and for persisting the raw output.
// taskID, when non-zero, registers the container under that task ID so that
// KillContainer(taskID) and log streaming work through the standard task paths.
// prompt is the full ideation prompt to send to the container; callers should
// generate it with buildIdeationPrompt() and persist it before calling here.
func (r *Runner) RunIdeation(ctx context.Context, taskID uuid.UUID, prompt string) ([]IdeateResult, *agentOutput, []byte, []byte, error) {
	containerName := fmt.Sprintf("wallfacer-ideate-%d", time.Now().UnixNano()/1e6)

	if taskID != uuid.Nil {
		r.taskContainers.Set(taskID, containerName)
		defer r.taskContainers.Delete(taskID)
	}
	r.ideateContainer.SetSingleton(containerName)
	defer r.ideateContainer.DeleteSingleton()

	exec.Command(r.command, "rm", "-f", containerName).Run()

	sandbox := "claude"
	if taskID != uuid.Nil {
		if task, err := r.store.GetTask(context.Background(), taskID); err == nil {
			sandbox = r.sandboxForTaskActivity(task, activityIdeaAgent)
		}
	}
	runWithSandbox := func(selectedSandbox string) (*agentOutput, []byte, []byte, error) {
		args := r.buildIdeationContainerArgs(containerName, prompt, selectedSandbox)
		cmd := exec.CommandContext(ctx, r.command, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		logger.Runner.Debug("ideate exec", "cmd", r.command, "args", strings.Join(args, " "), "sandbox", selectedSandbox)
		runErr := cmd.Run()

		if ctx.Err() != nil {
			exec.Command(r.command, "kill", containerName).Run()
			exec.Command(r.command, "rm", "-f", containerName).Run()
			return nil, stdout.Bytes(), stderr.Bytes(), fmt.Errorf("ideation container terminated: %w", ctx.Err())
		}

		raw := strings.TrimSpace(stdout.String())
		if raw == "" {
			if runErr != nil {
				if exitErr, ok := runErr.(*exec.ExitError); ok {
					return nil, stdout.Bytes(), stderr.Bytes(), fmt.Errorf("container exited %d: stderr=%s", exitErr.ExitCode(), stderr.String())
				}
				return nil, stdout.Bytes(), stderr.Bytes(), fmt.Errorf("exec container: %w", runErr)
			}
			return nil, stdout.Bytes(), stderr.Bytes(), fmt.Errorf("empty output from ideation container")
		}

		output, parseErr := parseOutput(raw)
		if parseErr != nil {
			return nil, stdout.Bytes(), stderr.Bytes(), fmt.Errorf("parse ideation output: %w", parseErr)
		}
		if output == nil || output.Result == "" {
			return nil, stdout.Bytes(), stderr.Bytes(), fmt.Errorf("no result in ideation output")
		}
		return output, stdout.Bytes(), stderr.Bytes(), nil
	}

	output, rawStdout, rawStderr, err := runWithSandbox(sandbox)
	if err != nil {
		if strings.EqualFold(sandbox, "claude") && isLikelyTokenLimitError(err.Error(), string(rawStderr), string(rawStdout)) {
			logger.Runner.Warn("ideation: claude token limit hit; retrying with codex", "task", taskID)
			output, rawStdout, rawStderr, err = runWithSandbox("codex")
		}
		if err != nil {
			return nil, nil, rawStdout, rawStderr, err
		}
	}

	if strings.EqualFold(sandbox, "claude") && output != nil && output.IsError &&
		isLikelyTokenLimitError(output.Result, output.Subtype) {
		logger.Runner.Warn("ideation: claude output reported token limit; retrying with codex", "task", taskID)
		retryOutput, retryStdout, retryStderr, retryErr := runWithSandbox("codex")
		if retryErr == nil {
			output = retryOutput
			rawStdout = retryStdout
			rawStderr = retryStderr
		}
	}

	ideas, err := extractIdeas(output.Result)
	if err != nil {
		return nil, output, rawStdout, rawStderr, fmt.Errorf("extract ideas: %w (result: %s)", err, truncate(output.Result, 300))
	}
	return ideas, output, rawStdout, rawStderr, nil
}

// BuildIdeationPrompt exposes the ideation prompt construction used by the
// idea-agent runner for testability and for handler-side task bootstrap.
func (r *Runner) BuildIdeationPrompt(existingTasks []store.Task) string {
	return buildIdeationPrompt(existingTasks)
}

// buildIdeationContainerArgs builds the container run arguments for the
// ideation agent. Workspaces are mounted read-only; no task label, no
// worktrees, and no board context are used.
func (r *Runner) buildIdeationContainerArgs(containerName, prompt, sandbox string) []string {
	args := []string{"run", "--rm", "--network=host", "--name", containerName}

	if r.envFile != "" {
		args = append(args, "--env-file", r.envFile)
	}

	if m := r.modelFromEnvForSandbox(sandbox); m != "" {
		args = append(args, "-e", "CLAUDE_CODE_MODEL="+m)
	}

	args = append(args, "-v", "claude-config:/home/claude/.claude")
	if hostPath := r.hostCodexAuthPath(); strings.EqualFold(strings.TrimSpace(sandbox), "codex") && hostPath != "" {
		args = append(args, "-v", hostPath+":/home/codex/.codex:z,ro")
	}

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
			args = append(args, "-v", r.instructionsPath+":/workspace/"+instructionsFilenameForSandbox(sandbox)+":z,ro")
		}
	}

	workdir := "/workspace"
	if len(basenames) == 1 {
		workdir = "/workspace/" + basenames[0]
	}
	args = append(args, "-w", workdir, r.sandboxImageForSandbox(sandbox))
	args = append(args, "-p", prompt, "--verbose", "--output-format", "stream-json")
	if m := r.modelFromEnvForSandbox(sandbox); m != "" {
		args = append(args, "--model", m)
	}

	return args
}

// runIdeationTask executes the brainstorm agent for an idea-agent task card.
// It runs RunIdeation, creates backlog tasks from the results, and transitions
// the idea-agent task to done. On failure it returns an error so Run() can
// transition the task to failed.
func (r *Runner) runIdeationTask(ctx context.Context, task *store.Task) error {
	bgCtx := context.Background()
	taskID := task.ID

	// Set a human-readable title on the idea-agent card.
	title := "Brainstorm " + time.Now().Format("Jan 2, 2006 15:04")
	r.store.UpdateTaskTitle(bgCtx, taskID, title)

	// Collect tasks currently in backlog, in_progress, or waiting so the
	// brainstorm agent can avoid proposing duplicates or conflicting ideas.
	allTasks, _ := r.store.ListTasks(bgCtx, false)
	var activeTasks []store.Task
	for _, t := range allTasks {
		if t.ID == taskID {
			continue // skip the brainstorm task itself
		}
		if t.Kind == store.TaskKindIdeaAgent {
			continue // skip other brainstorm meta-tasks
		}
		switch t.Status {
		case store.TaskStatusBacklog, store.TaskStatusInProgress, store.TaskStatusWaiting:
			activeTasks = append(activeTasks, t)
		}
	}

	// Generate the full ideation prompt (with randomly-picked domains).
	// Persist it as ExecutionPrompt so the UI can display the exact prompt
	// that was used while keeping Prompt semantics unchanged.
	ideationPrompt := buildIdeationPrompt(activeTasks)
	if err := r.store.UpdateTaskExecutionPrompt(bgCtx, taskID, ideationPrompt); err != nil {
		logger.Runner.Warn("ideation task: set execution prompt on brainstorm card", "task", taskID, "error", err)
	}

	r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
		"result": "Starting brainstorm agent — exploring workspaces to propose ideas...",
	})

	ideas, output, rawStdout, rawStderr, err := r.RunIdeation(ctx, taskID, ideationPrompt)

	// Always persist the raw container output as turn 1 so that the trace and
	// oversight features work the same as for regular implementation tasks.
	if len(rawStdout) > 0 {
		if saveErr := r.store.SaveTurnOutput(taskID, 1, rawStdout, rawStderr); saveErr != nil {
			logger.Runner.Warn("ideation: save turn output", "task", taskID, "error", saveErr)
		}
	}

	// Emit an output event and persist the agent result so the task card shows
	// the brainstorm summary and the Turns counter is non-zero (enabling oversight).
	if output != nil {
		sessionID := output.SessionID
		r.store.InsertEvent(bgCtx, taskID, store.EventTypeOutput, map[string]string{
			"result":      output.Result,
			"stop_reason": output.StopReason,
			"session_id":  sessionID,
		})
		r.store.UpdateTaskResult(bgCtx, taskID, output.Result, sessionID, output.StopReason, 1)
		r.store.AccumulateSubAgentUsage(bgCtx, taskID, "ideation", store.TaskUsage{
			InputTokens:          output.Usage.InputTokens,
			OutputTokens:         output.Usage.OutputTokens,
			CacheReadInputTokens: output.Usage.CacheReadInputTokens,
			CacheCreationTokens:  output.Usage.CacheCreationInputTokens,
			CostUSD:              output.TotalCostUSD,
		})
	} else {
		// No parsed output (e.g. container error before producing JSON); still
		// increment Turns so the trace file is indexed if stdout was non-empty.
		if len(rawStdout) > 0 {
			r.store.UpdateTaskTurns(bgCtx, taskID, 1)
		}
	}

	if err != nil {
		return err
	}

	r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
		"result": fmt.Sprintf("Brainstorm complete — creating %d idea task(s).", len(ideas)),
	})

	// Create a backlog task for each proposed idea.
	// The card's Prompt is set to the full implementation text.
	// ExecutionPrompt is also set so the sandbox uses the full details
	// even if the Prompt field is later edited.
	var titles []string
	for _, idea := range ideas {
		tags := []string{"idea-agent"}
		if idea.Category != "" {
			tags = append(tags, idea.Category)
		}
		// Use the full implementation prompt as the card prompt.
		cardPrompt := idea.Prompt
		if cardPrompt == "" {
			cardPrompt = idea.Title // fallback: use title if prompt is missing
		}
		newTask, createErr := r.store.CreateTask(bgCtx, cardPrompt, 60, false, "", store.TaskKindTask, tags...)
		if createErr != nil {
			logger.Runner.Warn("ideation task: create idea task", "task", taskID, "error", createErr)
			continue
		}
		r.store.InsertEvent(bgCtx, newTask.ID, store.EventTypeStateChange, map[string]string{
			"to": string(store.TaskStatusBacklog),
		})
		if idea.Title != "" {
			r.store.UpdateTaskTitle(bgCtx, newTask.ID, idea.Title)
			titles = append(titles, idea.Title)
		}
		// Also set ExecutionPrompt so the sandbox always receives the full details
		// even if the user edits the Prompt field before running the task.
		if err := r.store.UpdateTaskExecutionPrompt(bgCtx, newTask.ID, idea.Prompt); err != nil {
			logger.Runner.Warn("ideation task: set execution prompt", "task", newTask.ID, "error", err)
		}
		r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
			"result": fmt.Sprintf("Created idea task: %s", idea.Title),
		})
	}

	// Store a summary of proposed ideas as the task result so the card
	// displays what was generated without requiring a click-through.
	// Pass turns=1 to preserve the turn count set by the earlier UpdateTaskResult call.
	if len(titles) > 0 {
		var sb strings.Builder
		for _, title := range titles {
			sb.WriteString("- ")
			sb.WriteString(title)
			sb.WriteString("\n")
		}
		r.store.UpdateTaskResult(bgCtx, taskID, strings.TrimSpace(sb.String()), "", "", 1)
	}

	return nil
}

// extractIdeas finds a JSON array in the agent's text output and parses it
// into a slice of IdeateResult. It is tolerant of surrounding prose by
// scanning for the first '[' and then counting bracket depth to find its
// matching ']', which avoids capturing stray brackets in trailing prose.
func extractIdeas(text string) ([]IdeateResult, error) {
	start := strings.Index(text, "[")
	if start == -1 {
		return nil, fmt.Errorf("no JSON array found in agent output")
	}

	// Walk forward from the opening '[' counting bracket depth to find
	// the matching ']'. This is safe for JSON because brackets inside
	// strings are always escaped or paired, and we only care about
	// finding the correct closing bracket for the top-level array.
	depth := 0
	end := -1
	inString := false
	escaped := false
	for i := start; i < len(text); i++ {
		ch := text[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if ch == '[' {
			depth++
		} else if ch == ']' {
			depth--
			if depth == 0 {
				end = i
				break
			}
		}
	}
	if end == -1 {
		return nil, fmt.Errorf("no JSON array found in agent output")
	}

	var results []IdeateResult
	if err := json.Unmarshal([]byte(text[start:end+1]), &results); err != nil {
		return nil, fmt.Errorf("unmarshal ideas: %w", err)
	}

	// Filter out any malformed entries.
	// An idea where prompt equals the title is a degenerate output: the agent
	// copied the title into the prompt field instead of writing an implementation
	// spec. Reject these so runIdeationTask fails loudly rather than silently
	// creating tasks with no actionable implementation details.
	var valid []IdeateResult
	for _, r := range results {
		title := strings.TrimSpace(r.Title)
		prompt := strings.TrimSpace(r.Prompt)
		if title == "" || prompt == "" {
			continue
		}
		if strings.EqualFold(title, prompt) {
			continue // prompt is just the title — not a useful implementation spec
		}
		valid = append(valid, r)
	}
	if len(valid) == 0 {
		return nil, fmt.Errorf("no valid ideas in parsed output (all entries were malformed or had prompt equal to title)")
	}
	return valid, nil
}
