package runner

import (
	"context"
	"fmt"
	"io"
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
func (r *Runner) GenerateAgentSessionTitle(ctx context.Context, firstUserMessage string) (string, error) {
	if strings.TrimSpace(firstUserMessage) == "" {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	sb := r.sandboxFromEnvForActivity(activityTitle)
	if sb == "" {
		sb = harness.Claude
	}
	model := r.titleModelFromEnvForSandbox(sb)
	containerName := "wallfacer-planttitle-" + uuid.NewString()[:8]
	titlePrompt := r.promptsMgr.Title(firstUserMessage)
	labels := map[string]string{"wallfacer.task.activity": "title_planning"}

	spec := r.buildBaseContainerSpec(containerName, model, sb)
	spec.Labels = labels
	spec.Cmd = buildAgentCmd(titlePrompt, model)

	handle, err := r.backend.Launch(ctx, spec)
	if err != nil {
		return "", fmt.Errorf("launch agent-session title container: %w", err)
	}
	rawStdout, _ := io.ReadAll(handle.Stdout())
	_, _ = io.ReadAll(handle.Stderr())
	_, _ = handle.Wait()

	raw := strings.TrimSpace(string(rawStdout))
	if raw == "" {
		return "", fmt.Errorf("empty title output")
	}
	output, err := r.parseAgentStream(sb, raw)
	if err != nil {
		return "", fmt.Errorf("parse title output: %w", err)
	}
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(output.Result), `"'`)), nil
}
