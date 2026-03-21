package runner

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/sandbox"
	"changkun.de/wallfacer/internal/store"
	"changkun.de/wallfacer/prompts"
	"github.com/google/uuid"
)

const (
	maxIdeationIdeas           = 3
	ideationCandidateCount     = 6
	defaultIdeationImpactScore = 60
	maxIdeationChurnSignals     = 6
	maxIdeationTodoSignals      = 6
	workspaceIdeationCommandTTL = 2 * time.Second

	churnLookbackDays = 90  // only include commits newer than this many days
	maxChurnCommits   = 200 // hard cap so very active repos don't scan unboundedly
)

type ideationContext struct {
	FailureSignals     []string
	ChurnSignals       []prompts.WorkspaceSignal
	TodoSignals        []prompts.WorkspaceSignal
	FilteredChurnCount int
	FilteredTodoCount  int
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
	"security hardening",
	"observability / debugging",
	"data model / storage",
	"architecture / design",
}

// IdeationCategories returns the full inspiration pool used when generating
// brainstorm prompts.
func (r *Runner) IdeationCategories() []string {
	result := make([]string, len(ideaCategoryPool))
	copy(result, ideaCategoryPool)
	return result
}

// IdeationIgnorePatterns returns the ordered list of path prefixes excluded from
// workspace signal collection. This is exposed via GET /api/config so clients can
// understand and reproduce the filtering without reading source code.
func (r *Runner) IdeationIgnorePatterns() []string {
	result := make([]string, len(IdeationIgnorePatterns))
	copy(result, IdeationIgnorePatterns)
	return result
}

// pickCategoriesForInspiration returns a broader set of categories for the
// generate-then-rank pipeline. Unlike pickCategories which assigned one
// category per idea slot, this provides a larger pool purely as inspiration.
func pickCategoriesForInspiration() []string {
	pool := make([]string, len(ideaCategoryPool))
	copy(pool, ideaCategoryPool)
	for i := len(pool) - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		pool[i], pool[j] = pool[j], pool[i]
	}
	// Show 5-6 categories as inspiration, enough variety without overwhelming.
	n := 5
	if len(pool) > 5 {
		n = 5 + rand.Intn(2) // 5 or 6
	}
	if n > len(pool) {
		n = len(pool)
	}
	return pool[:n]
}

// buildIdeationPrompt constructs the full ideation prompt using a
// generate-then-rank pipeline: the agent brainstorms 6 candidates, self-
// critiques them against concrete impact criteria, and outputs the top 3.
// A broad set of inspiration categories is shown but the agent is free to
// propose multiple ideas in the same domain.
// existingTasks lists tasks currently in backlog, in_progress, or waiting state
// so the agent can avoid proposing duplicates or conflicting ideas.
func (r *Runner) buildIdeationPrompt(existingTasks []store.Task, contexts ...ideationContext) string {
	var signals ideationContext
	if len(contexts) > 0 {
		signals = contexts[0]
	}

	cats := pickCategoriesForInspiration()

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

	var rejectedTitles []string
	if r.store != nil {
		if hist, err := LoadHistory(r.store.DataDir()); err == nil {
			rejectedTitles = hist.RejectedTitles()
		} else {
			logger.Runner.Warn("buildIdeationPrompt: load history", "error", err)
		}
	}

	return r.promptsMgr.Ideation(prompts.IdeationData{
		ExistingTasks:      tasks,
		Categories:         cats,
		FailureSignals:     signals.FailureSignals,
		ChurnHotspots:      signals.ChurnSignals,
		TodoHotspots:       signals.TodoSignals,
		FilteredChurnCount: signals.FilteredChurnCount,
		FilteredTodoCount:  signals.FilteredTodoCount,
		RejectedTitles:     rejectedTitles,
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
func (r *Runner) RunIdeation(ctx context.Context, taskID uuid.UUID, prompt string) ([]IdeateResult, []ideaRejection, *agentOutput, []byte, []byte, error) { //nolint:revive // ideaRejection is intentionally unexported

	containerName := fmt.Sprintf("wallfacer-ideate-%d", time.Now().UnixNano()/1e6)

	if taskID != uuid.Nil {
		r.taskContainers.Set(taskID, containerName)
		defer r.taskContainers.Delete(taskID)
	}
	r.ideateContainer.SetSingleton(containerName)
	defer r.ideateContainer.DeleteSingleton()

	sb := sandbox.Claude
	if taskID != uuid.Nil {
		if task, err := r.store.GetTask(r.shutdownCtx, taskID); err == nil {
			sb = r.sandboxForTaskActivity(task, activityIdeaAgent)
		}
	}
	runWithSandbox := func(selectedSandbox sandbox.Type) (*agentOutput, []byte, []byte, error) {
		args := r.buildIdeationContainerArgs(containerName, prompt, selectedSandbox)

		logger.Runner.Debug("ideate exec", "cmd", r.command, "args", strings.Join(args, " "), "sandbox", selectedSandbox)
		if taskID != uuid.Nil {
			_ = r.store.InsertEvent(ctx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "container_run", Label: string(store.SandboxActivityIdeaAgent)})

		}
		rawStdout, rawStderr, runErr := r.executor.RunArgs(ctx, containerName, args)
		if taskID != uuid.Nil {
			_ = r.store.InsertEvent(ctx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "container_run", Label: string(store.SandboxActivityIdeaAgent)})

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

	output, rawStdout, rawStderr, err := runWithSandbox(sb)
	if err != nil {
		if sb == sandbox.Claude && isLikelyTokenLimitError(err.Error(), string(rawStderr), string(rawStdout)) {
			logger.Runner.Warn("ideation: claude token limit hit; retrying with codex", "task", taskID)
			output, rawStdout, rawStderr, err = runWithSandbox(sandbox.Codex)
		}
		if err != nil {
			return nil, nil, nil, rawStdout, rawStderr, err
		}
	}

	if sb == sandbox.Claude && output != nil && output.IsError &&
		isLikelyTokenLimitError(output.Result, output.Subtype) {
		logger.Runner.Warn("ideation: claude output reported token limit; retrying with codex", "task", taskID)
		retryOutput, retryStdout, retryStderr, retryErr := runWithSandbox(sandbox.Codex)
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
func (r *Runner) buildIdeationContainerArgs(containerName, prompt string, sb sandbox.Type) []string {
	model := r.modelFromEnvForSandbox(sb)
	spec := r.buildBaseContainerSpec(containerName, model, sb)

	var basenames []string
	if len(r.workspaces) > 0 {
		for _, ws := range r.workspaces {
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

	spec.Volumes = r.appendInstructionsMount(spec.Volumes, sb)

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
	_ = r.store.UpdateTaskTitle(bgCtx, taskID, title)


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

	_ = r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

		"result": "Starting brainstorm agent — exploring workspaces to propose ideas...",
	})

	ideas, rejections, output, rawStdout, rawStderr, err := r.RunIdeation(ctx, taskID, ideationPrompt)
	r.emitIdeationRejectionEvents(bgCtx, taskID, rejections)

	// Record rejected ideas to history so they are excluded from future prompts.
	hist, histErr := LoadHistory(r.store.DataDir())
	if histErr != nil {
		logger.Runner.Warn("ideation task: load history for recording", "task", taskID, "error", histErr)
		hist = nil
	}
	if hist != nil {
		for _, rej := range rejections {
			if rej.Title == "" {
				continue
			}
			he := HistoryEntry{
				Title:      rej.Title,
				Reason:     "rejected_" + string(rej.Reason),
				RecordedAt: time.Now().UTC(),
			}
			if appErr := hist.Append(he); appErr != nil {
				logger.Runner.Warn("ideation task: append rejection to history", "title", rej.Title, "error", appErr)
			}
		}
	}

	// Always persist the raw container output as turn 1 so that the trace and
	// oversight features work the same as for regular implementation tasks.
	if len(rawStdout) > 0 {
		if saveErr := r.store.SaveTurnOutput(taskID, 1, rawStdout, rawStderr); saveErr != nil {
			logger.Runner.Warn("ideation: save turn output", "task", taskID, "error", saveErr)
		}
		if len(rawStderr) > 0 {
			_ = r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

				"stderr_file": "turn-0001.stderr.txt",
				"turn":        "1",
			})
		}
	}

	// Emit an output event and persist the agent result so the task card shows
	// the brainstorm summary and the Turns counter is non-zero (enabling oversight).
	if output != nil {
		sessionID := output.SessionID
		_ = r.store.InsertEvent(bgCtx, taskID, store.EventTypeOutput, map[string]string{

			"result":      output.Result,
			"stop_reason": output.StopReason,
			"session_id":  sessionID,
		})
		_ = r.store.UpdateTaskResult(bgCtx, taskID, output.Result, sessionID, output.StopReason, 1)

		_ = r.store.AccumulateSubAgentUsage(bgCtx, taskID, store.SandboxActivityIdeaAgent, store.TaskUsage{

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
	} else if len(rawStdout) > 0 {
		// No parsed output (e.g. container error before producing JSON); still
		// increment Turns so the trace file is indexed if stdout was non-empty.
		_ = r.store.UpdateTaskTurns(bgCtx, taskID, 1)

	}

	if err != nil {
		return err
	}

	// Build the summary from parsed ideas and store it as the task result.
	summary := ideaSummaryLines(ideas)
	if len(summary) > 0 {
		var sb strings.Builder
		for _, line := range summary {
			sb.WriteString("- ")
			sb.WriteString(line)
			sb.WriteString("\n")
		}
		_ = r.store.UpdateTaskResult(bgCtx, taskID, strings.TrimSpace(sb.String()), "", "", 1)

	} else {
		_ = r.store.UpdateTaskResult(bgCtx, taskID, "No idea reached the minimum impact threshold.", "", "", 1)

	}

	// When auto-submit is enabled, create backlog tasks immediately.
	// Otherwise the brainstorm task will move to waiting for manual review;
	// backlog tasks are created when the user approves (moves to done).
	if r.isAutosubmitEnabled() {
		_ = r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

			"result": fmt.Sprintf("Brainstorm complete — creating %d idea task(s).", len(ideas)),
		})
		r.createIdeaBacklogTasks(bgCtx, taskID, ideas)
	} else {
		_ = r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

			"result": fmt.Sprintf("Brainstorm complete — %d idea(s) proposed. Approve to create backlog tasks.", len(ideas)),
		})
	}

	return nil
}

// ideaSummaryLines builds a display label for each idea.
func ideaSummaryLines(ideas []IdeateResult) []string {
	var lines []string
	for _, idea := range ideas {
		label := fmt.Sprintf("[%s %d] %s", idea.Priority, idea.ImpactScore, idea.Title)
		if idea.Priority == "" {
			label = idea.Title
		}
		lines = append(lines, label)
	}
	return lines
}

// createIdeaBacklogTasks creates a backlog task for each proposed idea and
// records accepted ideas in history.
func (r *Runner) createIdeaBacklogTasks(ctx context.Context, parentTaskID uuid.UUID, ideas []IdeateResult) {
	hist, histErr := LoadHistory(r.store.DataDir())
	if histErr != nil {
		logger.Runner.Warn("ideation: load history for recording", "task", parentTaskID, "error", histErr)
		hist = nil
	}

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
		cardPrompt := idea.Prompt
		if cardPrompt == "" {
			cardPrompt = idea.Title
		}
		newTask, createErr := r.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: cardPrompt, Timeout: 60, Kind: store.TaskKindTask, Tags: tags})
		if createErr != nil {
			logger.Runner.Warn("ideation task: create idea task", "task", parentTaskID, "error", createErr)
			continue
		}
		if hist != nil && idea.Title != "" {
			he := HistoryEntry{
				Title:      idea.Title,
				Reason:     "accepted",
				TaskID:     newTask.ID.String(),
				RecordedAt: time.Now().UTC(),
			}
			if appErr := hist.Append(he); appErr != nil {
				logger.Runner.Warn("ideation task: append accepted idea to history", "title", idea.Title, "error", appErr)
			}
		}
		_ = r.store.InsertEvent(ctx, newTask.ID, store.EventTypeStateChange,

			store.NewStateChangeData("", store.TaskStatusBacklog, "", nil))
		if idea.Title != "" {
			_ = r.store.UpdateTaskTitle(ctx, newTask.ID, idea.Title)

		}
		if err := r.store.UpdateTaskExecutionPrompt(ctx, newTask.ID, idea.Prompt); err != nil {
			logger.Runner.Warn("ideation task: set execution prompt", "task", newTask.ID, "error", err)
		}
		label := fmt.Sprintf("[%s %d] %s", idea.Priority, idea.ImpactScore, idea.Title)
		if idea.Priority == "" {
			label = idea.Title
		}
		_ = r.store.InsertEvent(ctx, parentTaskID, store.EventTypeSystem, map[string]string{

			"result": fmt.Sprintf("Created idea task: %s", label),
		})
	}
}

// CreateIdeaBacklogTasks re-extracts ideas from a completed brainstorm task's
// turn output and creates backlog tasks from them. This is used when the user
// manually approves a brainstorm that was held at waiting status.
func (r *Runner) CreateIdeaBacklogTasks(ctx context.Context, taskID uuid.UUID) error {
	// The raw agent output is stored in the turn output file; the task Result
	// field contains only the summary lines. Re-extract from the turn output.
	turnFile := r.store.OutputsDir(taskID) + "/turn-0001.json"
	rawStdout, readErr := os.ReadFile(turnFile)
	if readErr != nil {
		return fmt.Errorf("read turn output: %w", readErr)
	}

	var rawStderr []byte
	stderrFile := r.store.OutputsDir(taskID) + "/turn-0001.stderr.txt"
	if data, err := os.ReadFile(stderrFile); err == nil {
		rawStderr = data
	}

	output, parseErr := parseOutput(strings.TrimSpace(string(rawStdout)))
	if parseErr != nil {
		return fmt.Errorf("parse output: %w", parseErr)
	}

	ideas, _, extractErr := extractIdeasFromRunOutput(output.Result, rawStdout, rawStderr)
	if extractErr != nil {
		return fmt.Errorf("extract ideas: %w", extractErr)
	}

	_ = r.store.InsertEvent(ctx, taskID, store.EventTypeSystem, map[string]string{

		"result": fmt.Sprintf("Approved — creating %d idea task(s).", len(ideas)),
	})
	r.createIdeaBacklogTasks(ctx, taskID, ideas)
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
	churnSignals, filteredChurn := r.collectWorkspaceChurnSignals(ctx)
	todoSignals, filteredTodo := r.collectWorkspaceTodoSignals(ctx)
	return ideationContext{
		FailureSignals:     collectIdeationFailureSignals(tasks),
		ChurnSignals:       churnSignals,
		TodoSignals:        todoSignals,
		FilteredChurnCount: filteredChurn,
		FilteredTodoCount:  filteredTodo,
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
