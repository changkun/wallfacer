package runner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/sandbox"
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
	sb := r.sandboxForTaskActivity(task, activityTitle)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	titlePrompt := r.promptsMgr.Title(prompt)

	// runWithSandbox executes the title container with the given sandbox,
	// returning the parsed output (or nil) and any error.
	type titleResult struct {
		output *agentOutput
		err    error
		model  string
		sb     sandbox.Type
	}
	runWithSandbox := func(selected sandbox.Type) titleResult {
		mdl := r.titleModelFromEnvForSandbox(selected)
		containerName := "wallfacer-title-" + taskID.String()[:8]
		exec.Command(r.command, "rm", "-f", containerName).Run()

		spec := r.buildBaseContainerSpec(containerName, mdl, selected)
		spec.Cmd = buildAgentCmd(titlePrompt, mdl)

		cmd := exec.CommandContext(ctx, r.command, spec.Build()...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		r.store.InsertEvent(context.Background(), taskID, store.EventTypeSpanStart, store.SpanData{Phase: "container_run", Label: string(store.SandboxActivityTitle)})
		runErr := cmd.Run()
		r.store.InsertEvent(context.Background(), taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "container_run", Label: string(store.SandboxActivityTitle)})

		if ctx.Err() != nil {
			return titleResult{err: fmt.Errorf("container terminated: %w", ctx.Err()), model: mdl, sb: selected}
		}

		raw := strings.TrimSpace(stdout.String())
		if raw == "" {
			if runErr != nil {
				return titleResult{err: fmt.Errorf("%w: stderr=%s", runErr, truncate(stderr.String(), 200)), model: mdl, sb: selected}
			}
			return titleResult{err: fmt.Errorf("empty output"), model: mdl, sb: selected}
		}

		parsed, parseErr := parseOutput(raw)
		if parseErr != nil {
			if runErr != nil {
				return titleResult{
					err:   fmt.Errorf("%w: stderr=%s stdout=%s", runErr, truncate(stderr.String(), 200), truncate(raw, 200)),
					model: mdl,
					sb:    selected,
				}
			}
			return titleResult{err: fmt.Errorf("parse failure: raw=%s", truncate(raw, 200)), model: mdl, sb: selected}
		}
		if runErr != nil {
			if exitErr, ok := runErr.(*exec.ExitError); ok {
				logger.Runner.Warn("title generation: container exited non-zero but produced valid output",
					"task", taskID, "code", exitErr.ExitCode(), "sandbox", selected, "model", mdl)
			} else {
				logger.Runner.Warn("title generation: container error but produced valid output",
					"task", taskID, "error", runErr, "sandbox", selected, "model", mdl)
			}
		}
		return titleResult{output: parsed, model: mdl, sb: selected}
	}

	res := runWithSandbox(sb)

	// Fallback: if the claude sandbox hit a rate/token limit, retry with codex.
	if sb == sandbox.Claude && res.err != nil &&
		isLikelyTokenLimitError(res.err.Error()) {
		logger.Runner.Warn("title generation: claude sandbox token limit hit; retrying with codex",
			"task", taskID)
		r.store.InsertEvent(ctx, taskID, store.EventTypeSystem, map[string]string{
			"result": "Sandbox fallback: claude → codex (token/rate limit hit during title generation)",
		})
		sb = sandbox.Codex
		res = runWithSandbox(sandbox.Codex)
	}
	if sb == sandbox.Claude && res.output != nil && res.output.IsError &&
		isLikelyTokenLimitError(res.output.Result, res.output.Subtype) {
		logger.Runner.Warn("title generation: claude sandbox reported token limit in output; retrying with codex",
			"task", taskID)
		r.store.InsertEvent(ctx, taskID, store.EventTypeSystem, map[string]string{
			"result": "Sandbox fallback: claude → codex (token/rate limit in title output)",
		})
		sb = sandbox.Codex
		res = runWithSandbox(sandbox.Codex)
	}

	if res.err != nil {
		logger.Runner.Warn("title generation failed", "task", taskID, "sandbox", res.sb, "model", res.model, "error", res.err)
		return
	}
	output := res.output

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
		if err := r.store.AccumulateSubAgentUsage(context.Background(), taskID, store.SandboxActivityTitle, store.TaskUsage{
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
			Sandbox:              sb,
			SubAgent:             store.SandboxActivityTitle,
		}); err != nil {
			logger.Runner.Warn("title generation: append turn usage failed", "task", taskID, "error", err)
		}
	}
}
