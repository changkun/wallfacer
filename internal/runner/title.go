package runner

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// GenerateTitle runs a lightweight container to produce a 2-5 word title
// summarising the task prompt, then persists it via the store.
// Errors are logged and silently dropped so callers can fire-and-forget.
func (r *Runner) GenerateTitle(taskID uuid.UUID, prompt string) {
	task, err := r.store.GetTask(context.Background(), taskID)
	if err != nil {
		logger.Runner.Warn("GenerateTitle get task failed", "task", taskID, "error", err)
		return
	}
	if task.Title != "" {
		return
	}
	sandbox := r.sandboxForTaskActivity(task, activityTitle)
	model := r.titleModelFromEnvForSandbox(sandbox)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	containerName := "wallfacer-title-" + taskID.String()[:8]
	exec.Command(r.command, "rm", "-f", containerName).Run()

	spec := r.buildBaseContainerSpec(containerName, model, sandbox)

	titlePrompt := "Respond with ONLY a 2-5 word title that captures the main goal of the following task. " +
		"No punctuation, no quotes, no explanation — just the title.\n\nTask:\n" + prompt
	spec.Cmd = buildAgentCmd(titlePrompt, model)

	cmd := exec.CommandContext(ctx, r.command, spec.Build()...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	r.store.InsertEvent(context.Background(), taskID, store.EventTypeSpanStart, store.SpanData{Phase: "container_run", Label: store.SandboxActivityTitle})
	runErr := cmd.Run()
	r.store.InsertEvent(context.Background(), taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "container_run", Label: store.SandboxActivityTitle})
	if runErr != nil && ctx.Err() == nil {
		logger.Runner.Warn("title generation failed", "task", taskID, "error", runErr,
			"stderr", truncate(stderr.String(), 200))
		return
	}

	raw := strings.TrimSpace(stdout.String())
	if raw == "" {
		logger.Runner.Warn("title generation: empty output", "task", taskID)
		return
	}

	output, err := parseOutput(raw)
	if err != nil {
		logger.Runner.Warn("title generation: parse failure", "task", taskID, "raw", truncate(raw, 200))
		return
	}

	title := strings.TrimSpace(output.Result)
	title = strings.Trim(title, `"'`)
	title = strings.TrimSpace(title)
	if title == "" {
		logger.Runner.Warn("title generation: blank result", "task", taskID)
		return
	}

	if err := r.store.UpdateTaskTitle(context.Background(), taskID, title); err != nil {
		logger.Runner.Warn("title generation: store update failed", "task", taskID, "error", err)
	}

	// Accumulate token/cost usage for the title generation sub-agent.
	if output.Usage.InputTokens > 0 || output.Usage.OutputTokens > 0 || output.TotalCostUSD > 0 {
		if err := r.store.AccumulateSubAgentUsage(context.Background(), taskID, "title", store.TaskUsage{
			InputTokens:          output.Usage.InputTokens,
			OutputTokens:         output.Usage.OutputTokens,
			CacheReadInputTokens: output.Usage.CacheReadInputTokens,
			CacheCreationTokens:  output.Usage.CacheCreationInputTokens,
			CostUSD:              output.TotalCostUSD,
		}); err != nil {
			logger.Runner.Warn("title generation: accumulate usage failed", "task", taskID, "error", err)
		}
		if err := r.store.AppendTurnUsage(taskID, store.TurnUsageRecord{
			Turn:                 1,
			Timestamp:            time.Now().UTC(),
			InputTokens:          output.Usage.InputTokens,
			OutputTokens:         output.Usage.OutputTokens,
			CacheReadInputTokens: output.Usage.CacheReadInputTokens,
			CacheCreationTokens:  output.Usage.CacheCreationInputTokens,
			CostUSD:              output.TotalCostUSD,
			Sandbox:              sandbox,
			SubAgent:             "title",
		}); err != nil {
			logger.Runner.Warn("title generation: append turn usage failed", "task", taskID, "error", err)
		}
	}
}
