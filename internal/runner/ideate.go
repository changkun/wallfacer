package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"fmt"
	"math/rand"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
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
func buildIdeationPrompt(existingTasks []store.Task, contexts ...ideationContext) string {
	var signals ideationContext
	if len(contexts) > 0 {
		signals = contexts[0]
	}

	cats := pickCategories(3)
	var sb strings.Builder
	sb.WriteString(`You are a software development advisor reviewing the repositories in /workspace/. Your task is to propose exactly 3 improvements — each from a different assigned domain.

First, explore the workspace thoroughly:
- Read README files, AGENTS.md (or legacy CLAUDE.md), go.mod, package.json, or similar project manifests
- Scan the main source directories and read key source files to understand current patterns and pain points
- Review recent git history (git log --oneline -20) to see what has changed recently
- Identify concrete opportunities, rough edges, and gaps in the code

`)

	if len(signals.FailureSignals) > 0 {
		sb.WriteString("Objective failure signals (highest priority):\n")
		for _, item := range signals.FailureSignals {
			sb.WriteString("  - " + item + "\n")
		}
		sb.WriteString("\n")
	}

	if len(signals.ChurnSignals) > 0 {
		sb.WriteString("Recent-file churn hotspots (prioritize these):\n")
		for _, item := range signals.ChurnSignals {
			sb.WriteString("  - " + item + "\n")
		}
		sb.WriteString("\n")
	}

	if len(signals.TodoSignals) > 0 {
		sb.WriteString("TODO/FIXME concentration hotspots:\n")
		for _, item := range signals.TodoSignals {
			sb.WriteString("  - " + item + "\n")
		}
		sb.WriteString("\n")
	}

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
	sb.WriteString(`Then propose exactly 3 improvements, each covering a distinct area of the project.

Suggested focus areas (use as inspiration — you are NOT limited to these):
`)
	for _, cat := range cats {
		sb.WriteString(fmt.Sprintf("  - %s\n", cat))
	}
	sb.WriteString(`
Choose whatever categories best reflect the actual opportunities you found. Any area that genuinely needs work is fair game — for example: documentation update, architecture / design, dependency management, accessibility, migration, changelog, or anything else relevant to this codebase. Do not force ideas into the suggested categories if you found more pressing needs elsewhere.

Requirements for each improvement:
- Prioritize the objective signals above and generate the highest-impact suggestions first.
- Include an objective impact estimate and confidence level so the runner can make backlog decisions.
- Technically precise: name the specific files, functions, data structures, or API endpoints you observed during exploration — do not stay generic
- Creative and non-obvious: avoid safe, predictable suggestions like "add more tests" or "improve error handling" in isolation; propose something with genuine engineering interest
- Actionable end-to-end: write a prompt detailed enough for an AI coding agent to implement the full change without asking follow-up questions
- Non-duplicating: do not propose ideas that overlap or conflict with the existing active tasks listed above

Output ONLY a JSON array with exactly 3 objects. No preamble, no explanation, no markdown — just the JSON array:
[
  {
    "title": "2-5 word title",
    "category": "the area you chose for this idea",
    "priority": "high | medium | low",
    "impact_score": 0-100,
    "scope": "specific files/subsystems this idea touches",
    "rationale": "why this is higher impact than alternatives",
    "prompt": "Detailed implementation prompt referencing specific files, functions, and patterns found during exploration."
  },
  {
    "title": "...",
    "category": "a different area",
    "priority": "high | medium | low",
    "impact_score": 0-100,
    "scope": "...",
    "rationale": "...",
    "prompt": "..."
  },
  {
    "title": "...",
    "category": "yet another distinct area",
    "priority": "high | medium | low",
    "impact_score": 0-100,
    "scope": "...",
    "rationale": "...",
    "prompt": "..."
  }
]`)
	return sb.String()
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
	return buildIdeationPrompt(existingTasks, r.collectIdeationContext())
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

	// Generate the full ideation prompt (with randomly-picked domains).
	// Persist it as ExecutionPrompt so the UI can display the exact prompt
	// that was used while keeping Prompt semantics unchanged.
	ideationPrompt := buildIdeationPrompt(activeTasks, r.collectIdeationContextFromTasks(allTasks))
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

	// Normalize schema and filter out malformed entries.
	// An idea where prompt equals the title is a degenerate output: the agent
	// copied the title into the prompt field instead of writing an implementation
	// spec. Reject these so runIdeationTask fails loudly rather than silently
	// creating tasks with no actionable implementation details.
	var valid []IdeateResult
	seen := map[string]struct{}{}
	for _, r := range results {
		title := strings.TrimSpace(r.Title)
		prompt := strings.TrimSpace(r.Prompt)
		if title == "" || prompt == "" {
			continue
		}
		if strings.EqualFold(title, prompt) {
			continue // prompt is just the title — not a useful implementation spec
		}
		idea := r
		normalizeIdeationImpact(&idea)
		idea.Title = title
		idea.Prompt = prompt
		if idea.ImpactScore < minIdeationImpactScore {
			continue
		}
		if isIdeaDuplicateTitle(seen, idea.Title) {
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
		return nil, fmt.Errorf("no valid ideas in parsed output (all entries were malformed or had prompt equal to title)")
	}
	return valid, nil
}
