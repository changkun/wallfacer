package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os/exec"
	"sort"
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
		if taskID != uuid.Nil {
			r.store.InsertEvent(ctx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "container_run", Label: store.SandboxActivityIdeaAgent})
		}
		runErr := cmd.Run()
		if taskID != uuid.Nil {
			r.store.InsertEvent(ctx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "container_run", Label: store.SandboxActivityIdeaAgent})
		}

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
	return r.buildIdeationPrompt(existingTasks, r.collectIdeationContext())
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

	// Generate the ideation prompt (prefer the prebuilt execution prompt stored on
	// the idea-agent card for consistency).
	ideationPrompt := strings.TrimSpace(task.ExecutionPrompt)
	if ideationPrompt == "" {
		ideationPrompt = r.buildIdeationPrompt(activeTasks, r.collectIdeationContextFromTasks(allTasks))
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
func (r *Runner) collectIdeationContext() ideationContext {
	tasks, err := r.store.ListTasks(context.Background(), false)
	if err != nil {
		return r.collectIdeationContextFromTasks(nil)
	}
	return r.collectIdeationContextFromTasks(tasks)
}

func (r *Runner) collectIdeationContextFromTasks(tasks []store.Task) ideationContext {
	ctx := ideationContext{
		FailureSignals: collectIdeationFailureSignals(tasks),
		ChurnSignals:   r.collectWorkspaceChurnSignals(),
		TodoSignals:    r.collectWorkspaceTodoSignals(),
	}
	return ctx
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

func (r *Runner) collectWorkspaceChurnSignals() []string {
	var signals []string
	for _, workspace := range r.workspacesForRunner() {
		sig := r.collectWorkspaceChurnSignalsForWorkspace(workspace)
		signals = append(signals, sig...)
	}
	if len(signals) <= maxIdeationChurnSignals {
		return signals
	}
	return signals[:maxIdeationChurnSignals]
}

func (r *Runner) collectWorkspaceChurnSignalsForWorkspace(workspace string) []string {
	raw, err := r.runWorkspaceGitCommand(workspace, "log", "--name-only", "--pretty=format:", "-n", "30")
	if err != nil {
		return nil
	}
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return nil
	}
	counts := make(map[string]int)
	for _, line := range strings.Split(s, "\n") {
		file := strings.TrimSpace(line)
		if file == "" {
			continue
		}
		counts[file]++
	}
	type item struct {
		path  string
		count int
	}
	var list []item
	for path, count := range counts {
		list = append(list, item{path, count})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].count == list[j].count {
			return list[i].path < list[j].path
		}
		return list[i].count > list[j].count
	})
	maxItems := int(math.Min(float64(maxIdeationChurnSignals), float64(len(list))))
	out := make([]string, 0, maxItems)
	for i := 0; i < maxItems; i++ {
		out = append(out, fmt.Sprintf("%s (%d commits)", list[i].path, list[i].count))
	}
	return out
}

func (r *Runner) collectWorkspaceTodoSignals() []string {
	var signals []string
	for _, workspace := range r.workspacesForRunner() {
		sig := r.collectWorkspaceTodoSignalsForWorkspace(workspace)
		signals = append(signals, sig...)
	}
	if len(signals) <= maxIdeationTodoSignals {
		return signals
	}
	return signals[:maxIdeationTodoSignals]
}

func (r *Runner) collectWorkspaceTodoSignalsForWorkspace(workspace string) []string {
	raw, err := r.runWorkspaceGitCommand(workspace, "grep", "-n", "-E", "TODO|FIXME|XXX", "--", ".")
	if err != nil {
		return nil
	}
	counts := make(map[string]int)
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		before, _, found := strings.Cut(trimmed, ":")
		if !found || before == "" {
			continue
		}
		counts[before]++
	}
	type item struct {
		path  string
		count int
	}
	var list []item
	for path, count := range counts {
		list = append(list, item{path, count})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].count == list[j].count {
			return list[i].path < list[j].path
		}
		return list[i].count > list[j].count
	})
	maxItems := int(math.Min(float64(maxIdeationTodoSignals), float64(len(list))))
	out := make([]string, 0, maxItems)
	for i := 0; i < maxItems; i++ {
		out = append(out, fmt.Sprintf("%s (%d markers)", list[i].path, list[i].count))
	}
	return out
}

func (r *Runner) runWorkspaceGitCommand(workspace string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), workspaceIdeationCommandTTL)
	defer cancel()
	command := exec.CommandContext(ctx, "git", append([]string{"-C", workspace}, args...)...)
	return command.Output()
}

func (r *Runner) workspacesForRunner() []string {
	var ws []string
	for _, raw := range strings.Fields(r.workspaces) {
		clean := strings.TrimSpace(raw)
		if clean == "" {
			continue
		}
		ws = append(ws, clean)
	}
	return ws
}

func normalizeIdeationPriority(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "high", "p1", "critical", "urgent":
		return "high"
	case "medium", "med", "p2", "moderate":
		return "medium"
	case "low", "p3", "minor", "trivial":
		return "low"
	default:
		return ""
	}
}

func normalizeIdeationImpact(idea *IdeateResult) {
	idea.Priority = normalizeIdeationPriority(idea.Priority)
	if idea.ImpactScore < 0 {
		idea.ImpactScore = 0
	}
	if idea.ImpactScore > 100 {
		idea.ImpactScore = 100
	}
	if idea.ImpactScore == 0 {
		switch idea.Priority {
		case "high":
			idea.ImpactScore = 85
		case "medium":
			idea.ImpactScore = 60
		case "low":
			idea.ImpactScore = 35
		default:
			idea.ImpactScore = defaultIdeationImpactScore
		}
	}
	if idea.Priority == "" {
		switch {
		case idea.ImpactScore >= 80:
			idea.Priority = "high"
		case idea.ImpactScore >= 65:
			idea.Priority = "medium"
		default:
			idea.Priority = "low"
		}
	}
	idea.Scope = strings.TrimSpace(idea.Scope)
	idea.Rationale = strings.TrimSpace(idea.Rationale)
	idea.Category = strings.TrimSpace(idea.Category)
	if idea.Title == "" {
		idea.Title = strings.TrimSpace(idea.Title)
	}
	if idea.Prompt == "" {
		idea.Prompt = strings.TrimSpace(idea.Prompt)
	}
}

func isIdeaDuplicateTitle(added map[string]struct{}, title string) bool {
	current := strings.ToLower(strings.TrimSpace(title))
	if current == "" {
		return true
	}
	for existing := range added {
		if existing == current || strings.Contains(existing, current) || strings.Contains(current, existing) {
			return true
		}
	}
	added[current] = struct{}{}
	return false
}

type ideaRejection struct {
	Title  string
	Reason string
	Score  int
}

const (
	ideaRejectEmptyFields     = "empty_fields"
	ideaRejectDegenerateTitle = "degenerate_prompt"
	ideaRejectLowImpact       = "below_threshold"
	ideaRejectDuplicateTitle  = "duplicate_title"
)

func (r *Runner) emitIdeationRejectionEvents(ctx context.Context, taskID uuid.UUID, rejections []ideaRejection) {
	if len(rejections) == 0 {
		return
	}

	for _, rejection := range rejections {
		label := strings.TrimSpace(rejection.Title)
		if label == "" {
			label = "(untitled)"
		}
		r.store.InsertEvent(ctx, taskID, store.EventTypeSystem, map[string]string{
			"result": fmt.Sprintf("Idea filtered (%s): %q (score: %d)", rejection.Reason, label, rejection.Score),
		})
	}

	logger.Runner.Debug("ideation: idea filtering summary",
		"task", taskID,
		"rejections", len(rejections),
		"below_threshold", countIdeaRejections(rejections, ideaRejectLowImpact),
		"duplicate_title", countIdeaRejections(rejections, ideaRejectDuplicateTitle),
		"degenerate_prompt", countIdeaRejections(rejections, ideaRejectDegenerateTitle),
		"empty_fields", countIdeaRejections(rejections, ideaRejectEmptyFields),
	)
}

func countIdeaRejections(rejections []ideaRejection, reason string) int {
	total := 0
	for _, rejection := range rejections {
		if rejection.Reason == reason {
			total++
		}
	}
	return total
}

// extractIdeas finds a JSON array in the agent's text output and parses it
// into a slice of IdeateResult. It is tolerant of surrounding prose by
// scanning for the first '[' and then counting bracket depth to find its
// matching ']', which avoids capturing stray brackets in trailing prose.
func extractIdeas(text string) ([]IdeateResult, []ideaRejection, error) {
	candidates := extractJSONArrayLikeCandidates(text)
	var parseErr error
	var parseRejections []ideaRejection
	for _, candidate := range candidates {
		ideas, rejections, err := parseIdeaJSONArray(candidate)
		if err == nil {
			return ideas, rejections, nil
		}
		parseErr = err
		parseRejections = rejections
	}
	if parseErr != nil {
		return nil, parseRejections, parseErr
	}
	return nil, nil, fmt.Errorf("no JSON array found in agent output")
}

func extractJSONArrayLikeCandidates(text string) []string {
	candidates := make([]string, 0, 2)
	if text == "" {
		return candidates
	}
	// Accept JSON arrays embedded in prose (old behavior) and in fenced code
	// blocks (newer model variants sometimes wrap payloads in ```json).
	candidates = append(candidates, text)
	candidates = append(candidates, findJSONCodeBlock(text)...)
	return candidates
}

func parseIdeaJSONArray(text string) ([]IdeateResult, []ideaRejection, error) {
	start := strings.Index(text, "[")
	if start == -1 {
		return nil, nil, fmt.Errorf("no JSON array found in candidate output")
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
	var results []IdeateResult
	if end == -1 {
		repaired := repairTruncatedJSONArray(text, start)
		if repaired == "" {
			return nil, nil, fmt.Errorf("no JSON array found in candidate output")
		}
		logger.Runner.Warn("ideation: JSON array truncated; attempting partial recovery",
			"recovered_bytes", len(repaired))
		if err := json.Unmarshal([]byte(repaired), &results); err != nil {
			return nil, nil, fmt.Errorf("no JSON array found and partial recovery failed: %w", err)
		}
	} else {
		if err := json.Unmarshal([]byte(text[start:end+1]), &results); err != nil {
			return nil, nil, fmt.Errorf("unmarshal ideas: %w", err)
		}
	}

	// Normalize schema and filter out malformed entries.
	// An idea where prompt equals the title is a degenerate output: the agent
	// copied the title into the prompt field instead of writing an implementation
	// spec. Reject these so runIdeationTask fails loudly rather than silently
	// creating tasks with no actionable implementation details.
	var valid []IdeateResult
	var rejections []ideaRejection
	seen := map[string]struct{}{}
	for _, r := range results {
		title := strings.TrimSpace(r.Title)
		prompt := strings.TrimSpace(r.Prompt)
		if title == "" || prompt == "" {
			rejections = append(rejections, ideaRejection{
				Title:  title,
				Reason: ideaRejectEmptyFields,
			})
			continue
		}
		if strings.EqualFold(title, prompt) {
			rejections = append(rejections, ideaRejection{
				Title:  title,
				Reason: ideaRejectDegenerateTitle,
			})
			continue // prompt is just the title — not a useful implementation spec
		}
		idea := r
		normalizeIdeationImpact(&idea)
		idea.Title = title
		idea.Prompt = prompt
		if idea.ImpactScore < minIdeationImpactScore {
			rejections = append(rejections, ideaRejection{
				Title:  title,
				Score:  idea.ImpactScore,
				Reason: ideaRejectLowImpact,
			})
			continue
		}
		if isIdeaDuplicateTitle(seen, idea.Title) {
			rejections = append(rejections, ideaRejection{
				Title:  title,
				Score:  idea.ImpactScore,
				Reason: ideaRejectDuplicateTitle,
			})
			continue
		}
		valid = append(valid, idea)
	}
	sort.Slice(valid, func(i, j int) bool {
		if valid[i].ImpactScore == valid[j].ImpactScore {
			return valid[i].Title < valid[j].Title
		}
		return valid[i].ImpactScore > valid[j].ImpactScore
	})
	if len(valid) > maxIdeationIdeas {
		valid = valid[:maxIdeationIdeas]
	}
	if len(valid) == 0 {
		return nil, rejections, fmt.Errorf("no valid ideas in parsed output (all entries were malformed or had prompt equal to title)")
	}
	return valid, rejections, nil
}

// repairTruncatedJSONArray attempts to recover a valid JSON array from text
// that was cut off before the closing ']'. It scans forward from start
// tracking bracket depth and string state, recording every position where a
// complete top-level JSON object ends (i.e., a '}' that returns depth to 1
// while inside the outer array), then closes the array and returns the
// repaired string. Returns "" if no complete object is found.
func repairTruncatedJSONArray(text string, start int) string {
	// Walk forward tracking depth and string state, recording every position
	// where we return to depth==1 after a '}' (meaning one object just closed).
	depth := 0
	inString := false
	escaped := false
	lastObjEnd := -1
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
		switch ch {
		case '[', '{':
			depth++
		case '}':
			depth--
			if depth == 1 {
				lastObjEnd = i // closed an object inside the array
			}
		case ']':
			depth--
		}
	}
	if lastObjEnd == -1 {
		return ""
	}
	return text[start:lastObjEnd+1] + "]"
}

func findJSONCodeBlock(text string) []string {
	var blocks []string
	offset := 0
	for {
		start := strings.Index(text[offset:], "```")
		if start == -1 {
			return blocks
		}
		start += offset
		rest := text[start+3:]
		restOffset := strings.Index(rest, "\n")
		if restOffset == -1 {
			return blocks
		}
		firstLine := strings.TrimSpace(rest[:restOffset])
		// Some prompts use raw fences without language tag.
		contentStart := start + 3 + restOffset + 1
		end := strings.Index(text[contentStart:], "```")
		if end == -1 {
			return blocks
		}
		content := strings.TrimSpace(text[contentStart : contentStart+end])
		if firstLine == "" || strings.EqualFold(firstLine, "json") {
			blocks = append(blocks, content)
		}
		offset = contentStart + end + 3
	}
}

func extractIdeasFromRunOutput(result string, rawStdout, rawStderr []byte) ([]IdeateResult, []ideaRejection, error) {
	// Prefer the final parsed result if it already contains ideas.
	if ideas, rejections, err := extractIdeas(result); err == nil {
		return ideas, rejections, nil
	}

	text := strings.TrimSpace(string(rawStdout) + "\n" + string(rawStderr))
	if text == "" {
		return nil, nil, fmt.Errorf("no JSON array found in agent output")
	}

	var fallback []IdeateResult
	var fallbackRejections []ideaRejection
	var fallbackErr error
	var candidateRejections []ideaRejection
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var output agentOutput
		if err := json.Unmarshal([]byte(line), &output); err != nil {
			continue
		}
		if strings.TrimSpace(output.Result) == "" {
			continue
		}
		ideas, rejections, err := extractIdeas(output.Result)
		if err != nil {
			fallbackErr = err
			candidateRejections = append(candidateRejections, rejections...)
			continue
		}
		if output.StopReason != "" {
			candidateRejections = append(candidateRejections, rejections...)
			return ideas, candidateRejections, nil
		}
		if fallback == nil {
			fallback = ideas
			fallbackRejections = rejections
		}
	}
	if fallback != nil {
		return fallback, append(fallbackRejections, candidateRejections...), nil
	}
	if fallbackErr != nil {
		return nil, candidateRejections, fallbackErr
	}
	return nil, nil, fmt.Errorf("no JSON array found in agent output")
}
