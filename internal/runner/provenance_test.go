package runner

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"changkun.de/wallfacer/internal/store"
)

// TestCaptureExecutionEnvironment_ModelFromEnvconfig verifies that ModelName is
// populated from the env file when no per-task override is present.
func TestCaptureExecutionEnvironment_ModelFromEnvconfig(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte("CLAUDE_DEFAULT_MODEL=claude-test-model\n"), 0600); err != nil {
		t.Fatal(err)
	}

	r := NewRunner(nil, RunnerConfig{
		Command:      "echo",
		SandboxImage: "wallfacer:latest",
		EnvFile:      envFile,
	})

	task := store.Task{Sandbox: ""}
	env := r.captureExecutionEnvironment(task)

	if env.ModelName != "claude-test-model" {
		t.Errorf("ModelName = %q, want %q", env.ModelName, "claude-test-model")
	}
	if env.RecordedAt.IsZero() {
		t.Error("RecordedAt should not be zero")
	}
}

// TestCaptureExecutionEnvironment_InstructionsHash verifies that InstructionsHash
// is a valid 64-character hex string when the instructions file exists.
func TestCaptureExecutionEnvironment_InstructionsHash(t *testing.T) {
	instrFile := filepath.Join(t.TempDir(), "CLAUDE.md")
	content := []byte("# Workspace Instructions\n\nDo the thing.\n")
	if err := os.WriteFile(instrFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	r := NewRunner(nil, RunnerConfig{
		Command:          "echo",
		SandboxImage:     "wallfacer:latest",
		InstructionsPath: instrFile,
	})

	task := store.Task{}
	env := r.captureExecutionEnvironment(task)

	hexPattern := regexp.MustCompile(`^[0-9a-f]{64}$`)
	if !hexPattern.MatchString(env.InstructionsHash) {
		t.Errorf("InstructionsHash = %q, want 64-char lowercase hex", env.InstructionsHash)
	}
}

// TestCaptureExecutionEnvironment_MissingInstructions verifies that a missing
// instructions file leaves InstructionsHash empty without error.
func TestCaptureExecutionEnvironment_MissingInstructions(t *testing.T) {
	r := NewRunner(nil, RunnerConfig{
		Command:          "echo",
		SandboxImage:     "wallfacer:latest",
		InstructionsPath: "/nonexistent/path/CLAUDE.md",
	})

	task := store.Task{}
	env := r.captureExecutionEnvironment(task)

	if env.InstructionsHash != "" {
		t.Errorf("InstructionsHash = %q, want empty string when file does not exist", env.InstructionsHash)
	}
}

// TestCaptureExecutionEnvironment_ContainerDigestEmpty verifies that
// ContainerDigest is empty (not an error) when the image inspect command fails.
func TestCaptureExecutionEnvironment_ContainerDigestEmpty(t *testing.T) {
	// Using a command that will fail for an image that doesn't exist.
	r := NewRunner(nil, RunnerConfig{
		Command:      "false", // always exits non-zero
		SandboxImage: "wallfacer:latest",
	})

	task := store.Task{}
	env := r.captureExecutionEnvironment(task)

	if env.ContainerDigest != "" {
		t.Errorf("ContainerDigest = %q, want empty string on inspect failure", env.ContainerDigest)
	}
}

// TestCaptureExecutionEnvironment_ContainerImage verifies that ContainerImage
// is set to the resolved sandbox image name.
func TestCaptureExecutionEnvironment_ContainerImage(t *testing.T) {
	r := NewRunner(nil, RunnerConfig{
		Command:      "echo",
		SandboxImage: "wallfacer:latest",
	})

	task := store.Task{}
	env := r.captureExecutionEnvironment(task)

	if env.ContainerImage != "wallfacer:latest" {
		t.Errorf("ContainerImage = %q, want %q", env.ContainerImage, "wallfacer:latest")
	}
}

// TestCaptureExecutionEnvironment_Sandbox verifies that the Sandbox field is
// resolved via sandboxForTaskActivity (defaulting to "claude").
func TestCaptureExecutionEnvironment_Sandbox(t *testing.T) {
	r := NewRunner(nil, RunnerConfig{
		Command:      "echo",
		SandboxImage: "wallfacer:latest",
	})

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
		Command:      "echo",
		SandboxImage: "wallfacer:latest",
		EnvFile:      envFile,
	})

	task := store.Task{Model: "override-model"}
	env := r.captureExecutionEnvironment(task)

	if env.ModelName != "override-model" {
		t.Errorf("ModelName = %q, want %q", env.ModelName, "override-model")
	}
}
