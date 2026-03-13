package runner

import (
	"context"
	"fmt"
	"math/rand"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
	"changkun.de/wallfacer/prompts"
	"github.com/google/uuid"
)

const ideationTimeout = 10 * time.Minute
const (
	maxIdeationIdeas            = 3
	minIdeationImpactScore      = 60
	defaultIdeationImpactScore  = 60
	maxIdeationChurnSignals     = 6
	maxIdeationTodoSignals      = 6
	workspaceIdeationCommandTTL = 2 * time.Second
)

type ideationContext struct {
	FailureSignals []string
	ChurnSignals   []string
	TodoSignals    []string
}

// ideaCategoryPool is the set of example improvement areas shown to the
// brainstorm agent as inspiration. The agent is not confined to these — it
// may propose ideas in any category it discovers during workspace exploration.
// Sampling 3 unique entries per run seeds the brainstorm with variety while
// leaving the agent free to override any suggestion with something more relevant.
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
	"documentation update",
	"architecture / design",
	"dependency management",
	"accessibility",
}

// IdeationCategories returns the full inspiration pool used when generating
// brainstorm prompts.
func (r *Runner) IdeationCategories() []string {
	result := make([]string, len(ideaCategoryPool))
	copy(result, ideaCategoryPool)
	return result
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
func (r *Runner) buildIdeationPrompt(existingTasks []store.Task, contexts ...ideationContext) string {
	var signals ideationContext
	if len(contexts) > 0 {
		signals = contexts[0]
	}

	cats := pickCategories(3)

	var tasks []prompts.IdeationTask
	for _, t := range existingTasks {
		title := t.Title
		if title == "" {
			title = "(untitled)"
		}
		p := strings.TrimSpace(t.Prompt)
		if len(p) > 120 {
			p = p[:120] + "..."
		}
		tasks = append(tasks, prompts.IdeationTask{
			Title:  title,
			Status: string(t.Status),
			Prompt: p,
		})
	}

	return r.promptsMgr.Ideation(prompts.IdeationData{
		ExistingTasks:  tasks,
		Categories:     cats,
		FailureSignals: signals.FailureSignals,
		ChurnSignals:   signals.ChurnSignals,
		TodoSignals:    signals.TodoSignals,
	})
}

// IdeateResult holds a single idea proposed by the brainstorm agent.
type IdeateResult struct {
	Title       string `json:"title"`
	Category    string `json:"category"`
	Priority    string `json:"priority"`
	ImpactScore int    `json:"impact_score"`
	Scope       string `json:"scope"`
	Rationale   string `json:"rationale"`
	Prompt      string `json:"prompt"`
}

// RunIdeation runs a lightweight read-only container to analyse the workspaces
// and returns up to 3 proposed task ideas together with the raw container
// stdout/stderr and the parsed agent output. The caller is responsible for
// creating backlog tasks from the results and for persisting the raw output.
// taskID, when non-zero, registers the container under that task ID so that
// KillContainer(taskID) and log streaming work through the standard task paths.
// prompt is the full ideation prompt to send to the container; callers should
// generate it with buildIdeationPrompt() and persist it before calling here.
func (r *Runner) RunIdeation(ctx context.Context, taskID uuid.UUID, prompt string) ([]IdeateResult, []ideaRejection, *agentOutput, []byte, []byte, error) {
	containerName := fmt.Sprintf("wallfacer-ideate-%d", time.Now().UnixNano()/1e6)

	if taskID != uuid.Nil {
		r.taskContainers.Set(taskID, containerName)
		defer r.taskContainers.Delete(taskID)
	}
	r.ideateContainer.SetSingleton(containerName)
	defer r.ideateContainer.DeleteSingleton()

	sandbox := "claude"
	if taskID != uuid.Nil {
		if task, err := r.store.GetTask(context.Background(), taskID); err == nil {
			sandbox = r.sandboxForTaskActivity(task, activityIdeaAgent)
		}
	}
	runWithSandbox := func(selectedSandbox string) (*agentOutput, []byte, []byte, error) {
		args := r.buildIdeationContainerArgs(containerName, prompt, selectedSandbox)

		logger.Runner.Debug("ideate exec", "cmd", r.command, "args", strings.Join(args, " "), "sandbox", selectedSandbox)
		if taskID != uuid.Nil {
			r.store.InsertEvent(ctx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "container_run", Label: store.SandboxActivityIdeaAgent})
		}
		rawStdout, rawStderr, runErr := r.executor.RunArgs(ctx, containerName, args)
		if taskID != uuid.Nil {
			r.store.InsertEvent(ctx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "container_run", Label: store.SandboxActivityIdeaAgent})
		}

		if ctx.Err() != nil {
			r.executor.Kill(containerName)
			return nil, rawStdout, rawStderr, fmt.Errorf("ideation container terminated: %w", ctx.Err())
		}

		raw := strings.TrimSpace(string(rawStdout))
		if raw == "" {
			if runErr != nil {
				if exitErr, ok := runErr.(*exec.ExitError); ok {
					return nil, rawStdout, rawStderr, fmt.Errorf("container exited %d: stderr=%s", exitErr.ExitCode(), string(rawStderr))
				}
				return nil, rawStdout, rawStderr, fmt.Errorf("exec container: %w", runErr)
			}
			return nil, rawStdout, rawStderr, fmt.Errorf("empty output from ideation container")
		}

		output, parseErr := parseOutput(raw)
		if parseErr != nil {
			return nil, rawStdout, rawStderr, fmt.Errorf("parse ideation output: %w", parseErr)
		}
		if output == nil || output.Result == "" {
			return nil, rawStdout, rawStderr, fmt.Errorf("no result in ideation output")
		}
		return output, rawStdout, rawStderr, nil
	}

	output, rawStdout, rawStderr, err := runWithSandbox(sandbox)
	if err != nil {
		if strings.EqualFold(sandbox, "claude") && isLikelyTokenLimitError(err.Error(), string(rawStderr), string(rawStdout)) {
			logger.Runner.Warn("ideation: claude token limit hit; retrying with codex", "task", taskID)
			output, rawStdout, rawStderr, err = runWithSandbox("codex")
		}
		if err != nil {
			return nil, nil, nil, rawStdout, rawStderr, err
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

	ideas, rejections, err := extractIdeas(output.Result)
	if err != nil {
		recovered, recoveredRejections, recoverErr := extractIdeasFromRunOutput(output.Result, rawStdout, rawStderr)
		if recoverErr == nil {
			ideas = recovered
			rejections = recoveredRejections
			err = nil
		} else {
			return nil, nil, output, rawStdout, rawStderr, fmt.Errorf("extract ideas: %w (result: %s)", err, truncate(output.Result, 300))
		}
	}
	return ideas, rejections, output, rawStdout, rawStderr, nil
}

// BuildIdeationPrompt exposes the ideation prompt construction used by the
// idea-agent runner for testability and for handler-side task bootstrap.
func (r *Runner) BuildIdeationPrompt(existingTasks []store.Task) string {
	return r.buildIdeationPrompt(existingTasks, r.collectIdeationContext(r.shutdownCtx))
}

// buildIdeationContainerArgs builds the container run arguments for the
// ideation agent. Workspaces are mounted read-only; no task label, no
// worktrees, and no board context are used.
func (r *Runner) buildIdeationContainerArgs(containerName, prompt, sandbox string) []string {
	model := r.modelFromEnvForSandbox(sandbox)
	spec := r.buildBaseContainerSpec(containerName, model, sandbox)

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
			spec.Volumes = append(spec.Volumes, VolumeMount{
				Host:      ws,
				Container: "/workspace/" + basename,
				Options:   "z,ro",
			})
		}
	}

	spec.Volumes = r.appendInstructionsMount(spec.Volumes, sandbox)

	spec.WorkDir = workdirForBasenames(basenames)
	spec.Cmd = buildAgentCmd(prompt, model)

	return spec.Build()
}

// runIdeationTask executes the brainstorm agent for an idea-agent task card.
// It runs RunIdeation, creates backlog tasks from the results, and transitions
// the idea-agent task to done. On failure it returns an error so Run() can
// transition the task to failed.
func (r *Runner) runIdeationTask(ctx context.Context, task *store.Task) error {
	bgCtx := r.shutdownCtx
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

	// Generate the ideation prompt (prefer the prebuilt execution prompt stored on
	// the idea-agent card for consistency).
	ideationPrompt := strings.TrimSpace(task.ExecutionPrompt)
	if ideationPrompt == "" {
		ideationPrompt = r.buildIdeationPrompt(activeTasks, r.collectIdeationContextFromTasks(bgCtx, allTasks))
		if err := r.store.UpdateTaskExecutionPrompt(bgCtx, taskID, ideationPrompt); err != nil {
			logger.Runner.Warn("ideation task: set execution prompt on brainstorm card", "task", taskID, "error", err)
		}
	}

	r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
		"result": "Starting brainstorm agent — exploring workspaces to propose ideas...",
	})

	ideas, rejections, output, rawStdout, rawStderr, err := r.RunIdeation(ctx, taskID, ideationPrompt)
	r.emitIdeationRejectionEvents(bgCtx, taskID, rejections)

	// Always persist the raw container output as turn 1 so that the trace and
	// oversight features work the same as for regular implementation tasks.
	if len(rawStdout) > 0 {
		if saveErr := r.store.SaveTurnOutput(taskID, 1, rawStdout, rawStderr); saveErr != nil {
			logger.Runner.Warn("ideation: save turn output", "task", taskID, "error", saveErr)
		}
		if len(rawStderr) > 0 {
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
				"stderr_file": "turn-0001.stderr.txt",
				"turn":        "1",
			})
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
		r.store.AccumulateSubAgentUsage(bgCtx, taskID, store.SandboxActivityIdeaAgent, store.TaskUsage{
			InputTokens:          output.Usage.InputTokens,
			OutputTokens:         output.Usage.OutputTokens,
			CacheReadInputTokens: output.Usage.CacheReadInputTokens,
			CacheCreationTokens:  output.Usage.CacheCreationInputTokens,
			CostUSD:              output.TotalCostUSD,
		})
		if appErr := r.store.AppendTurnUsage(taskID, store.TurnUsageRecord{
			Turn:                 1,
			Timestamp:            time.Now().UTC(),
			InputTokens:          output.Usage.InputTokens,
			OutputTokens:         output.Usage.OutputTokens,
			CacheReadInputTokens: output.Usage.CacheReadInputTokens,
			CacheCreationTokens:  output.Usage.CacheCreationInputTokens,
			CostUSD:              output.TotalCostUSD,
			SubAgent:             store.SandboxActivityIdeaAgent,
		}); appErr != nil {
			logger.Runner.Warn("ideation: append turn usage failed", "task", taskID, "error", appErr)
		}
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
	var summary []string
	for _, idea := range ideas {
		tags := make([]string, 0, 4)
		tags = append(tags, "idea-agent")
		if idea.Category != "" {
			tags = append(tags, idea.Category)
		}
		if idea.Priority != "" {
			tags = append(tags, "priority:"+idea.Priority)
		}
		if idea.ImpactScore > 0 {
			tags = append(tags, "impact:"+strconv.Itoa(idea.ImpactScore))
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
		label := fmt.Sprintf("[%s %d] %s", idea.Priority, idea.ImpactScore, idea.Title)
		if idea.Priority == "" {
			label = idea.Title
		}
		titles = append(titles, idea.Title)
		summary = append(summary, label)
		r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
			"result": fmt.Sprintf("Created idea task: %s", label),
		})
	}

	// Store a summary of proposed ideas as the task result so the card
	// displays what was generated without requiring a click-through.
	// Pass turns=1 to preserve the turn count set by the earlier UpdateTaskResult call.
	if len(titles) > 0 {
		var sb strings.Builder
		for _, summaryLine := range summary {
			sb.WriteString("- ")
			sb.WriteString(summaryLine)
			sb.WriteString("\n")
		}
		r.store.UpdateTaskResult(bgCtx, taskID, strings.TrimSpace(sb.String()), "", "", 1)
	} else {
		r.store.UpdateTaskResult(bgCtx, taskID, "No idea reached the minimum impact threshold.", "", "", 1)
	}

	return nil
}

// collectIdeationContext returns workspace and task-derived signals for prompt
// construction so ideation suggestions can be prioritized by objective urgency.
func (r *Runner) collectIdeationContext(ctx context.Context) ideationContext {
	tasks, err := r.store.ListTasks(ctx, false)
	if err != nil {
		return r.collectIdeationContextFromTasks(ctx, nil)
	}
	return r.collectIdeationContextFromTasks(ctx, tasks)
}

func (r *Runner) collectIdeationContextFromTasks(ctx context.Context, tasks []store.Task) ideationContext {
	return ideationContext{
		FailureSignals: collectIdeationFailureSignals(tasks),
		ChurnSignals:   r.collectWorkspaceChurnSignals(ctx),
		TodoSignals:    r.collectWorkspaceTodoSignals(ctx),
	}
}

func collectIdeationFailureSignals(tasks []store.Task) []string {
	type failureSignal struct {
		label string
	}
	signals := make([]failureSignal, 0, len(tasks))
	seen := map[string]struct{}{}
	for _, task := range tasks {
		if task.Kind == store.TaskKindIdeaAgent {
			continue
		}
		isFail := strings.EqualFold(task.LastTestResult, "fail") || task.Status == store.TaskStatusFailed
		if !isFail {
			continue
		}

		title := strings.TrimSpace(task.Title)
		if title == "" {
			title = strings.TrimSpace(task.Prompt)
		}
		if title == "" {
			title = "(untitled)"
		}
		if _, ok := seen[title]; ok {
			continue
		}
		seen[title] = struct{}{}
		reason := "failed"
		if strings.EqualFold(task.LastTestResult, "fail") {
			reason = "last test result: fail"
		}
		signals = append(signals, failureSignal{label: fmt.Sprintf("%s (%s)", title, reason)})
		if len(signals) >= maxIdeationIdeas {
			break
		}
	}
	result := make([]string, 0, len(signals))
	for _, s := range signals {
		result = append(result, s.label)
	}
	return result
}

