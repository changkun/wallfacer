package runner

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// GenerateTitle runs a lightweight container to produce a 2-5 word title
// summarising the task prompt, then persists it via the store.
// Errors are logged and silently dropped so callers can fire-and-forget.
func (r *Runner) GenerateTitle(taskID uuid.UUID, prompt string) {
	task, err := r.taskStore(taskID).GetTask(r.shutdownCtx, taskID)
	if err != nil {
		logger.Runner.Warn("GenerateTitle get task failed", "task", taskID, "error", err)
		return
	}
	if task.Title != "" {
		return
	}
	sb := r.sandboxForTaskActivity(task, activityTitle)

	ctx, cancel := context.WithTimeout(r.shutdownCtx, 60*time.Second)
	defer cancel()

	titlePrompt := r.promptsMgr.Title(prompt)

	containerName := "wallfacer-title-" + taskID.String()[:8]
	r.taskContainers.Set(taskID, containerName)
	defer r.taskContainers.Delete(taskID)

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

		spec := r.buildBaseContainerSpec(containerName, mdl, selected)
		spec.Labels = map[string]string{"wallfacer.task.id": taskID.String()}
		spec.Cmd = buildAgentCmd(titlePrompt, mdl)

		_ = r.taskStore(taskID).InsertEvent(r.shutdownCtx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "container_run", Label: string(store.SandboxActivityTitle)})

		handle, launchErr := r.backend.Launch(ctx, spec)
		if launchErr != nil {
			_ = r.taskStore(taskID).InsertEvent(r.shutdownCtx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "container_run", Label: string(store.SandboxActivityTitle)})
			return titleResult{err: fmt.Errorf("launch title container: %w", launchErr), model: mdl, sb: selected}
		}
		r.taskContainers.SetHandle(taskID, handle, nil)

		rawStdout, _ := io.ReadAll(handle.Stdout())
		rawStderr, _ := io.ReadAll(handle.Stderr())
		exitCode, _ := handle.Wait()
		_ = r.taskStore(taskID).InsertEvent(r.shutdownCtx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "container_run", Label: string(store.SandboxActivityTitle)})

		if ctx.Err() != nil {
			_ = handle.Kill()
			return titleResult{err: fmt.Errorf("container terminated: %w", ctx.Err()), model: mdl, sb: selected}
		}

		raw := strings.TrimSpace(string(rawStdout))
		if raw == "" {
			if exitCode != 0 {
				return titleResult{err: fmt.Errorf("container exited with code %d: stderr=%s", exitCode, truncate(string(rawStderr), 200)), model: mdl, sb: selected}
			}
			return titleResult{err: fmt.Errorf("empty output"), model: mdl, sb: selected}
		}

		parsed, parseErr := parseOutput(raw)
		if parseErr != nil {
			if exitCode != 0 {
				return titleResult{
					err:   fmt.Errorf("container exited with code %d: stderr=%s stdout=%s", exitCode, truncate(string(rawStderr), 200), truncate(raw, 200)),
					model: mdl,
					sb:    selected,
				}
			}
			return titleResult{err: fmt.Errorf("parse failure: raw=%s", truncate(raw, 200)), model: mdl, sb: selected}
		}
		if exitCode != 0 {
			logger.Runner.Warn("title generation: container exited non-zero but produced valid output",
				"task", taskID, "code", exitCode, "sandbox", selected, "model", mdl)
		}
		return titleResult{output: parsed, model: mdl, sb: selected}
	}

	res := runWithSandbox(sb)

	// Fallback: if the claude sandbox hit a rate/token limit, retry with codex.
	if sb == sandbox.Claude && res.err != nil &&
		isLikelyTokenLimitError(res.err.Error()) {
		logger.Runner.Warn("title generation: claude sandbox token limit hit; retrying with codex",
			"task", taskID)
		_ = r.taskStore(taskID).InsertEvent(ctx, taskID, store.EventTypeSystem, map[string]string{

			"result": "Sandbox fallback: claude → codex (token/rate limit hit during title generation)",
		})
		sb = sandbox.Codex
		res = runWithSandbox(sandbox.Codex)
	}
	if sb == sandbox.Claude && res.output != nil && res.output.IsError &&
		isLikelyTokenLimitError(res.output.Result, res.output.Subtype) {
		logger.Runner.Warn("title generation: claude sandbox reported token limit in output; retrying with codex",
			"task", taskID)
		_ = r.taskStore(taskID).InsertEvent(ctx, taskID, store.EventTypeSystem, map[string]string{

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

	if err := r.taskStore(taskID).UpdateTaskTitle(r.shutdownCtx, taskID, title); err != nil {
		logger.Runner.Warn("title generation: store update failed", "task", taskID, "error", err)
	}

	// Accumulate token/cost usage for the title generation sub-agent.
	if output.Usage.InputTokens > 0 || output.Usage.OutputTokens > 0 || output.TotalCostUSD > 0 {
		if err := r.taskStore(taskID).AccumulateSubAgentUsage(r.shutdownCtx, taskID, store.SandboxActivityTitle, store.TaskUsage{
			InputTokens:          output.Usage.InputTokens,
			OutputTokens:         output.Usage.OutputTokens,
			CacheReadInputTokens: output.Usage.CacheReadInputTokens,
			CacheCreationTokens:  output.Usage.CacheCreationInputTokens,
			CostUSD:              output.TotalCostUSD,
		}); err != nil {
			logger.Runner.Warn("title generation: accumulate usage failed", "task", taskID, "error", err)
		}
		if err := r.taskStore(taskID).AppendTurnUsage(taskID, store.TurnUsageRecord{
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
