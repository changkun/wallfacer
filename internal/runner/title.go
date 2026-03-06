package runner

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/logger"
	"github.com/google/uuid"
)

// GenerateTitle runs a lightweight container to produce a 2-5 word title
// summarising the task prompt, then persists it via the store.
// Errors are logged and silently dropped so callers can fire-and-forget.
func (r *Runner) GenerateTitle(taskID uuid.UUID, prompt string) {
	// Skip if the task already has a title.
	if t, err := r.store.GetTask(context.Background(), taskID); err == nil && t.Title != "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	containerName := "wallfacer-title-" + taskID.String()[:8]
	exec.Command(r.command, "rm", "-f", containerName).Run()

	args := []string{"run", "--rm", "--network=host", "--name", containerName}
	if r.envFile != "" {
		args = append(args, "--env-file", r.envFile)
	}
	// Inject CLAUDE_CODE_MODEL so subagents use the configured model.
	if m := r.titleModelFromEnv(); m != "" {
		args = append(args, "-e", "CLAUDE_CODE_MODEL="+m)
	}
	args = append(args, "-v", "claude-config:/home/claude/.claude")
	args = append(args, r.sandboxImage)

	titlePrompt := "Respond with ONLY a 2-5 word title that captures the main goal of the following task. " +
		"No punctuation, no quotes, no explanation — just the title.\n\nTask:\n" + prompt
	args = append(args, "-p", titlePrompt, "--output-format", "stream-json", "--verbose")
	if model := r.titleModelFromEnv(); model != "" {
		args = append(args, "--model", model)
	}

	cmd := exec.CommandContext(ctx, r.command, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil && ctx.Err() == nil {
		logger.Runner.Warn("title generation failed", "task", taskID, "error", err,
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
}
