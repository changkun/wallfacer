package runner

import (
	"os"
	"path/filepath"
	"testing"

	"latere.ai/x/wallfacer/internal/store"
)

// TestCaptureExecutionEnvironment_ModelFromEnvconfig verifies that ModelName is
// populated from the env file when no per-task override is present.
func TestCaptureExecutionEnvironment_ModelFromEnvconfig(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte("CLAUDE_DEFAULT_MODEL=claude-test-model\n"), 0600); err != nil {
		t.Fatal(err)
	}

	r := NewRunner(nil, RunnerConfig{
		Command: "echo",
		EnvFile: envFile,
	})
	t.Cleanup(func() { r.Shutdown() })

	task := store.Task{Sandbox: ""}
	env := r.captureExecutionEnvironment(task)

	if env.ModelName != "claude-test-model" {
		t.Errorf("ModelName = %q, want %q", env.ModelName, "claude-test-model")
	}
	if env.RecordedAt.IsZero() {
		t.Error("RecordedAt should not be zero")
	}
}

// TestCaptureExecutionEnvironment_Sandbox verifies that the Sandbox field is
// resolved via sandboxForTaskActivity (defaulting to "claude").
func TestCaptureExecutionEnvironment_Sandbox(t *testing.T) {
	r := NewRunner(nil, RunnerConfig{
		Command: "echo",
	})
	t.Cleanup(func() { r.Shutdown() })

	// No sandbox set → defaults to "claude".
	env := r.captureExecutionEnvironment(store.Task{})
	if env.Sandbox != "claude" {
		t.Errorf("Sandbox = %q, want %q", env.Sandbox, "claude")
	}

	// Explicit sandbox.
	env = r.captureExecutionEnvironment(store.Task{Sandbox: "codex"})
	if env.Sandbox != "codex" {
		t.Errorf("Sandbox = %q, want %q", env.Sandbox, "codex")
	}
}

// TestCaptureExecutionEnvironment_TaskModelOverride verifies that a per-task
// Model field overrides the envconfig default.
func TestCaptureExecutionEnvironment_TaskModelOverride(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte("CLAUDE_DEFAULT_MODEL=default-model\n"), 0600); err != nil {
		t.Fatal(err)
	}

	r := NewRunner(nil, RunnerConfig{
		Command: "echo",
		EnvFile: envFile,
	})
	t.Cleanup(func() { r.Shutdown() })

	task := store.Task{Model: "override-model"}
	env := r.captureExecutionEnvironment(task)

	if env.ModelName != "override-model" {
		t.Errorf("ModelName = %q, want %q", env.ModelName, "override-model")
	}
}
