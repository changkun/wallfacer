package runner

import (
	"time"

	"latere.ai/x/wallfacer/internal/envconfig"
	"latere.ai/x/wallfacer/internal/store"
)

// captureExecutionEnvironment snapshots the runtime environment at the start of
// a task execution. The returned record is persisted via UpdateTaskEnvironment
// so that reproducibility auditing can identify what changed between runs.
// Individual field capture failures (missing env file, unavailable image digest)
// are silently tolerated: the environment is best-effort metadata that must
// never prevent a task from executing.
func (r *Runner) captureExecutionEnvironment(task store.Task) store.ExecutionEnvironment {
	env := store.ExecutionEnvironment{
		RecordedAt: time.Now(),
	}

	// Model: read from env config, with per-task override taking precedence.
	if r.envFile != "" {
		cfg, _ := envconfig.Parse(r.envFile)
		env.ModelName = r.modelFromEnvForSandbox(task.Sandbox)
		env.APIBaseURL = cfg.BaseURL
	}
	// Per-task model override (deprecated field, kept for migration compatibility).
	if task.Model != "" {
		env.ModelName = task.Model
	}

	// Sandbox: record the configured sandbox for this task.
	env.Sandbox = r.sandboxForTaskActivity(&task, activityImplementation)

	return env
}
