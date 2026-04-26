package runner

import (
	"strings"

	"github.com/google/uuid"

	"changkun.de/x/wallfacer/internal/agents"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/sandbox"
)

// parseTitleResult extracts the trimmed title string from an
// agentOutput.
func parseTitleResult(o *agentOutput) (any, error) {
	title := strings.TrimSpace(o.Result)
	title = strings.Trim(title, `"'`)
	return strings.TrimSpace(title), nil
}

// GenerateTitle runs a lightweight container to produce a 2-5 word
// title summarising the task prompt, then persists it via the store.
func (r *Runner) GenerateTitle(taskID uuid.UUID, prompt string) {
	task, err := r.taskStore(taskID).GetTask(r.shutdownCtx, taskID)
	if err != nil {
		logger.Runner.Warn("GenerateTitle get task failed", "task", taskID, "error", err)
		return
	}
	if task == nil {
		logger.Runner.Warn("GenerateTitle: task not found", "task", taskID)
		return
	}
	if task.Title != "" {
		return
	}

	titlePrompt := r.promptsMgr.Title(prompt)
	res, err := r.runAgent(r.shutdownCtx, agents.Title, task, titlePrompt, runAgentOpts{
		EmitSpanEvents: true,
		TrackUsage:     true,
		Turn:           1,
		ModelResolver:  func(sb sandbox.Type) string { return r.titleModelFromEnvForSandbox(sb) },
	})
	if err != nil {
		logger.Runner.Warn("title generation failed", "task", taskID, "error", err)
		return
	}

	title, _ := res.Parsed.(string)
	if title == "" {
		logger.Runner.Warn("title generation: blank result", "task", taskID)
		return
	}
	if err := r.taskStore(taskID).UpdateTaskTitle(r.shutdownCtx, taskID, title); err != nil {
		logger.Runner.Warn("title generation: store update failed", "task", taskID, "error", err)
	}
}
