package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/envconfig"
	"changkun.de/wallfacer/internal/store"
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

	// Container image: resolve using the same logic as the container runner.
	env.ContainerImage = r.sandboxImageForSandbox(task.Sandbox)

	// Container digest: query the runtime for the image's content digest.
	// Failures are non-fatal; digest is left empty when unavailable.
	if env.ContainerImage != "" && r.command != "" {
		out, err := exec.Command(r.command, "inspect", "--format", "{{.Digest}}", env.ContainerImage).Output()
		if err == nil {
			env.ContainerDigest = strings.TrimSpace(string(out))
		}
	}

	// Instructions hash: SHA-256 of the workspace CLAUDE.md file content.
	if r.instructionsPath != "" {
		data, err := os.ReadFile(r.instructionsPath)
		if err == nil {
			sum := sha256.Sum256(data)
			env.InstructionsHash = hex.EncodeToString(sum[:])
		}
	}

	return env
}
