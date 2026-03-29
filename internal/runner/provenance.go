package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"time"

	"changkun.de/x/wallfacer/internal/envconfig"
	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
	"changkun.de/x/wallfacer/internal/store"
)

// captureExecutionEnvironment snapshots the runtime environment at the start of
// a task execution. The returned record is persisted via UpdateTaskEnvironment
// so that reproducibility auditing can identify what changed between runs.
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

	// Container image: resolve using the same logic as the container runner.
	env.ContainerImage = r.sandboxImageForSandbox(task.Sandbox)

	// Container digest: query the runtime for the image's content digest.
	// Failures are non-fatal; digest is left empty when unavailable.
	if env.ContainerImage != "" && r.command != "" {
		out, err := cmdexec.New(r.command, "inspect", "--format", "{{.Digest}}", env.ContainerImage).Output()
		if err == nil {
			env.ContainerDigest = out
		}
	}

	// Instructions hash: SHA-256 of the workspace CLAUDE.md file content.
	instrPath := r.currentInstructionsPath()
	if instrPath != "" {
		data, err := os.ReadFile(instrPath)
		if err == nil {
			sum := sha256.Sum256(data)
			env.InstructionsHash = hex.EncodeToString(sum[:])
		}
	}

	return env
}
