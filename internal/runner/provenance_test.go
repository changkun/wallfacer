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
// model pin overrides the envconfig default, through both the live
// ModelOverride field and the deprecated Model spelling.
func TestCaptureExecutionEnvironment_TaskModelOverride(t *testing.T) {
	override := "override-model"
	cases := []struct {
		name string
		task store.Task
		want string
	}{
		{"model override", store.Task{ModelOverride: &override}, "override-model"},
		{"deprecated model field", store.Task{Model: "override-model"}, "override-model"},
		{"no pin falls back to env default", store.Task{}, "default-model"},
	}

	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte("CLAUDE_DEFAULT_MODEL=default-model\n"), 0600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRunner(nil, RunnerConfig{
				Command: "echo",
				EnvFile: envFile,
			})
			t.Cleanup(func() { r.Shutdown() })

			env := r.captureExecutionEnvironment(tc.task)
			if env.ModelName != tc.want {
				t.Errorf("ModelName = %q, want %q", env.ModelName, tc.want)
			}
		})
	}
}
