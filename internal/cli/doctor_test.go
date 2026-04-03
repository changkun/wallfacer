package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunDoctor_AllGood verifies the happy path: valid config dir, env file,
// and credential produce only [ok] lines and no issue warnings.
func TestRunDoctor_AllGood(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte("ANTHROPIC_API_KEY=sk-ant-test1234\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out := captureStdout(func() {
		RunDoctor(configDir)
	})

	if !strings.Contains(out, "[ok] Config directory") {
		t.Errorf("expected config dir ok, got:\n%s", out)
	}
	if !strings.Contains(out, "[ok] Env file") {
		t.Errorf("expected env file ok, got:\n%s", out)
	}
	if !strings.Contains(out, "[ok] ANTHROPIC_API_KEY is set") {
		t.Errorf("expected credential ok, got:\n%s", out)
	}
}

// TestRunDoctor_MissingConfigDir verifies doctor warns about a non-existent
// config directory and reports an issue count.
func TestRunDoctor_MissingConfigDir(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "nonexistent")

	out := captureStdout(func() {
		RunDoctor(configDir)
	})

	if !strings.Contains(out, "[!] Config directory missing") {
		t.Errorf("expected config dir warning, got:\n%s", out)
	}
	if !strings.Contains(out, "issue(s) found") {
		t.Errorf("expected issues summary, got:\n%s", out)
	}
}

// TestRunDoctor_NoCredentials verifies doctor warns when no Claude credential
// is configured in the .env file.
func TestRunDoctor_NoCredentials(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte("# empty\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out := captureStdout(func() {
		RunDoctor(configDir)
	})

	if !strings.Contains(out, "[!] No Claude credential") {
		t.Errorf("expected no-credential warning, got:\n%s", out)
	}
}

// TestRunDoctor_PlaceholderTokenIgnored verifies that the default template
// placeholder "your-oauth-token-here" is treated as missing, not as a valid credential.
func TestRunDoctor_PlaceholderTokenIgnored(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte("CLAUDE_CODE_OAUTH_TOKEN=your-oauth-token-here\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out := captureStdout(func() {
		RunDoctor(configDir)
	})

	if !strings.Contains(out, "[!] No Claude credential") {
		t.Errorf("expected placeholder to be treated as missing, got:\n%s", out)
	}
}

// TestRunDoctor_OAuthTokenWorks verifies that a real (non-placeholder) OAuth
// token is recognized as a valid Claude credential.
func TestRunDoctor_OAuthTokenWorks(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte("CLAUDE_CODE_OAUTH_TOKEN=real-oauth-token-value\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out := captureStdout(func() {
		RunDoctor(configDir)
	})

	if !strings.Contains(out, "[ok] CLAUDE_CODE_OAUTH_TOKEN is set") {
		t.Errorf("expected credential ok, got:\n%s", out)
	}
}

// TestRunDoctor_OpenAIOptional verifies that the optional OPENAI_API_KEY is
// reported as [ok] when present alongside an Anthropic credential.
func TestRunDoctor_OpenAIOptional(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte("ANTHROPIC_API_KEY=sk-ant-test\nOPENAI_API_KEY=sk-test\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out := captureStdout(func() {
		RunDoctor(configDir)
	})

	if !strings.Contains(out, "[ok] OPENAI_API_KEY is set") {
		t.Errorf("expected openai ok, got:\n%s", out)
	}
}

// TestRunDoctor_ContainerRuntimeNotFound verifies that doctor warns when the
// configured container runtime binary does not exist.
func TestRunDoctor_ContainerRuntimeNotFound(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte("ANTHROPIC_API_KEY=sk-ant-test\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Point to a non-existent container command.
	t.Setenv("CONTAINER_CMD", "/nonexistent/container-runtime")

	out := captureStdout(func() {
		RunDoctor(configDir)
	})

	if !strings.Contains(out, "[!] Container runtime not found") {
		t.Errorf("expected runtime warning, got:\n%s", out)
	}
}

// TestRunDoctor_RuntimeRespondingWithVersion verifies that doctor shows the
// container runtime version when the runtime binary responds.
func TestRunDoctor_RuntimeRespondingWithVersion(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte("ANTHROPIC_API_KEY=sk-ant-test\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Create a fake container runtime that responds with a version.
	fakeRuntime := filepath.Join(t.TempDir(), "podman")
	script := "#!/bin/sh\nif [ \"$1\" = \"version\" ]; then echo \"5.0.0\"; exit 0; fi\nif [ \"$1\" = \"images\" ]; then echo \"\"; exit 0; fi\nexit 0\n"
	if err := os.WriteFile(fakeRuntime, []byte(script), 0755); err != nil {
		t.Fatalf("write fake runtime: %v", err)
	}

	t.Setenv("CONTAINER_CMD", fakeRuntime)

	out := captureStdout(func() {
		RunDoctor(configDir)
	})

	if !strings.Contains(out, "[ok] Container runtime") {
		t.Errorf("expected container runtime ok, got:\n%s", out)
	}
}

// TestRunDoctor_RuntimeNotResponding verifies that doctor warns when the
// container runtime binary exists but doesn't respond.
func TestRunDoctor_RuntimeNotResponding(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte("ANTHROPIC_API_KEY=sk-ant-test\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Create a fake runtime that fails on version check.
	fakeRuntime := filepath.Join(t.TempDir(), "podman")
	script := "#!/bin/sh\nexit 1\n"
	if err := os.WriteFile(fakeRuntime, []byte(script), 0755); err != nil {
		t.Fatalf("write fake runtime: %v", err)
	}

	t.Setenv("CONTAINER_CMD", fakeRuntime)

	out := captureStdout(func() {
		RunDoctor(configDir)
	})

	if !strings.Contains(out, "not responding") {
		t.Errorf("expected 'not responding' warning, got:\n%s", out)
	}
}

// TestRunDoctor_ConfigDirIsFile verifies that doctor warns when the config
// path exists but is a regular file instead of a directory.
func TestRunDoctor_ConfigDirIsFile(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config")
	if err := os.WriteFile(configPath, []byte("not a dir"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	envFile := filepath.Join(tmp, ".env")
	if err := os.WriteFile(envFile, []byte("ANTHROPIC_API_KEY=sk-ant-test\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("ENV_FILE", envFile)
	t.Setenv("CONTAINER_CMD", "/nonexistent/runtime")

	out := captureStdout(func() {
		RunDoctor(configPath)
	})

	if !strings.Contains(out, "is not a directory") {
		t.Errorf("expected 'is not a directory' warning, got:\n%s", out)
	}
}

// TestRunDoctor_GitCheck verifies that doctor detects git on the system PATH
// (expected to be present in CI and development environments).
func TestRunDoctor_GitCheck(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte("ANTHROPIC_API_KEY=sk-ant-test\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out := captureStdout(func() {
		RunDoctor(configDir)
	})

	// Git should be found in CI and dev environments.
	if !strings.Contains(out, "[ok] git version") {
		t.Errorf("expected git ok, got:\n%s", out)
	}
}
