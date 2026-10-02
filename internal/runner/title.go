package runner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/agents"
	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/logger"
)

// parseTitleResult extracts the trimmed title string from an
// agentOutput.
func parseTitleResult(o *agentOutput) (any, error) {
	title := strings.TrimSpace(o.Result)
	title = strings.Trim(title, `"'`)
	return strings.TrimSpace(title), nil
}

// GenerateTitle runs a lightweight container to produce a 2-5 word
// title summarizing the task prompt, then persists it via the store.
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
	if err := r.taskStore(taskID).UpdateTaskTitleGenerating(r.shutdownCtx, taskID, true); err != nil {
		logger.Runner.Warn("title generation: publish start failed", "task", taskID, "error", err)
		return
	}
	defer func() {
		if err := r.taskStore(taskID).UpdateTaskTitleGenerating(r.shutdownCtx, taskID, false); err != nil {
			logger.Runner.Warn("title generation: publish finish failed", "task", taskID, "error", err)
		}
	}()

	titlePrompt := r.promptsMgr.Title(prompt)
	res, err := r.runAgent(r.shutdownCtx, agents.Title, task, titlePrompt, runAgentOpts{
		EmitSpanEvents: true,
		TrackUsage:     true,
		Turn:           1,
		ModelResolver:  func(sb harness.ID) string { return r.titleModelFromEnvForSandbox(sb) },
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

// GenerateAgentSessionTitle produces a short (2–5 word) title for an agent session
// chat thread from its opening user message, using the lightweight title model.
// Task-free, like GenerateCommitMessage: it records no spans or usage. A blank
// model response returns ("", nil) and should be treated as "no title".
//
// It runs the title role through runAgent, the path a task's title takes, so an
// in-process harness runs through the agent-graph seam rather than the
// executor, which cannot launch one. With no task to route by, the harness is
// the env file's title harness, else its default harness, else
// harness.Default(); it is handed to runAgent as the role's harness pin.
func (r *Runner) GenerateAgentSessionTitle(ctx context.Context, firstUserMessage string) (string, error) {
	if strings.TrimSpace(firstUserMessage) == "" {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	sb := r.sandboxFromEnvForActivity(activityTitle)
	if !sb.IsValid() {
		sb = harness.Default()
	}
	role := agents.Title
	role.Harness = string(sb)

	res, err := r.runAgent(ctx, role, nil, r.promptsMgr.Title(firstUserMessage), runAgentOpts{
		ContainerName: "wallfacer-planttitle-" + uuid.NewString()[:8],
		Labels:        map[string]string{"wallfacer.task.activity": "title_planning"},
		ModelResolver: r.titleModelFromEnvForSandbox,
	})
	if err != nil {
		return "", fmt.Errorf("agent-session title: %w", err)
	}
	title, _ := res.Parsed.(string)
	return title, nil
}
