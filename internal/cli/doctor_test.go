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
