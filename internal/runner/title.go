package runner

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
)

// roleTitle is the headless-tier descriptor for the title-generation
// sub-agent. Emits a 2–5 word summary of the task prompt; no mounts,
// single-turn, and uses the title-specific CLAUDE_TITLE_MODEL env var
// (falls back to CLAUDE_DEFAULT_MODEL) so operators can route title
// work to a cheaper model.
var roleTitle = AgentRole{
	Activity:    store.SandboxActivityTitle,
	Name:        "title",
	Timeout:     func(*store.Task) time.Duration { return constants.TitleAgentTimeout },
	MountMode:   MountNone,
	SingleTurn:  true,
	ParseResult: parseTitleResult,
}

// parseTitleResult extracts the trimmed title string from an agentOutput.
// The agent returns the title verbatim in `result`; we strip surrounding
// quotes and whitespace before persisting.
func parseTitleResult(o *agentOutput) (any, error) {
	title := strings.TrimSpace(o.Result)
	title = strings.Trim(title, `"'`)
	return strings.TrimSpace(title), nil
}

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

	// Bind the title-specific model resolver to this call rather than
	// the package-level descriptor so the resolver sees the runner
	// instance and can read its envconfig cache.
	role := roleTitle
	role.Model = func(sb sandbox.Type) string { return r.titleModelFromEnvForSandbox(sb) }

	titlePrompt := r.promptsMgr.Title(prompt)
	res, err := r.runAgent(r.shutdownCtx, role, task, titlePrompt, runAgentOpts{
		EmitSpanEvents: true,
		TrackUsage:     true,
		Turn:           1,
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
