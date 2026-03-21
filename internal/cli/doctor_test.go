package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
