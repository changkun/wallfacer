package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestPrintUsage validates that the help text written to stderr contains the
// expected command listing and usage header.
func TestPrintUsage(t *testing.T) {
	out := captureStderr(func() {
		PrintUsage()
	})
	for _, want := range []string{"Usage: wallfacer <command> [arguments]", "Commands:", "run          start the task board server"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected usage output to contain %q, got: %s", want, out)
		}
	}
}

// TestInitConfigDir_CreatesEnvTemplate verifies that initConfigDir creates the
// .env template on first call and leaves it untouched on subsequent calls.
func TestInitConfigDir_CreatesEnvTemplate(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")

	initConfigDir(configDir, envFile)

	raw, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "CLAUDE_CODE_OAUTH_TOKEN") {
		t.Fatalf("expected env template, got: %s", content)
	}
	if _, err := os.Stat(envFile); err != nil {
		t.Fatalf("expected env file exists: %v", err)
	}

	// Calling again should keep existing file intact.
	initConfigDir(configDir, envFile)
	if _, err := os.Stat(envFile); err != nil {
		t.Fatalf("expected env file after second call: %v", err)
	}
}

// TestRunDoctor_MissingEnvFile verifies that doctor reports the missing .env
// file warning when it does not exist.
func TestRunDoctor_MissingEnvFile(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	t.Setenv("ENV_FILE", envFile)
	t.Setenv("CONTAINER_CMD", "printf")
	t.Setenv("SANDBOX_IMAGE", "wallfacer-test:latest")

	out := captureStdout(func() {
		RunDoctor(configDir, nil)
	})
	for _, want := range []string{"Config directory:  " + configDir, "Env file:          " + envFile, "[!] Env file not found"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got: %s", want, out)
		}
	}
}

// TestRunDoctor_WithCredentials verifies that doctor recognizes both Claude and
// OpenAI credentials and optional URL settings from the .env file.
func TestRunDoctor_WithCredentials(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	content := strings.Join([]string{
		"CLAUDE_CODE_OAUTH_TOKEN=oauth-token-12345678",
		"OPENAI_API_KEY=openai-key-12345678",
		"ANTHROPIC_BASE_URL=https://api.anthropic.com",
		"OPENAI_BASE_URL=https://api.openai.com/v1",
	}, "\n")
	if err := os.WriteFile(envFile, []byte(content), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("ENV_FILE", envFile)
	t.Setenv("CONTAINER_CMD", "printf")
	t.Setenv("SANDBOX_IMAGE", "wallfacer-test:latest")

	out := captureStdout(func() {
		RunDoctor(configDir, nil)
	})
	for _, want := range []string{"[ok] CLAUDE_CODE_OAUTH_TOKEN is set", "[ok] OPENAI_API_KEY is set", "[ok] ANTHROPIC_BASE_URL = https://api.anthropic.com", "[ok] OPENAI_BASE_URL = https://api.openai.com/v1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got: %s", want, out)
		}
	}
}

// TestOpenBrowser_InvokesPlatformCommand installs a fake browser-open script on
// $PATH and verifies that openBrowser invokes it with the given URL.
func TestOpenBrowser_InvokesPlatformCommand(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "called")
	cmd := "xdg-open"
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "linux":
		cmd = "xdg-open"
	case "windows":
		t.Skip("openBrowser default is no-op on windows")
	default:
		cmd = "open"
	}
	script := filepath.Join(root, cmd)
	scriptBody := "#!/bin/sh\n" +
		"echo \"$1\" > " + marker
	if err := os.WriteFile(script, []byte(scriptBody+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", strings.Join([]string{root, os.Getenv("PATH")}, string(os.PathListSeparator)))

	openBrowser("http://localhost")

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("expected xdg-open helper to run")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestConfigDir_ReturnsPath verifies that ConfigDir returns a path ending
// in .wallfacer under the user's home directory.
func TestConfigDir_ReturnsPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot determine home dir: %v", err)
	}
	got := ConfigDir()
	want := filepath.Join(home, ".wallfacer")
	if got != want {
		t.Fatalf("ConfigDir() = %q, want %q", got, want)
	}
}

// TestPrintUsage_IncludesVersion verifies that PrintUsage includes the version
// string when Version is set.
func TestPrintUsage_IncludesVersion(t *testing.T) {
	old := Version
	Version = "2.0.0"
	defer func() { Version = old }()

	out := captureStderr(func() {
		PrintUsage()
	})
	if !strings.Contains(out, "wallfacer 2.0.0") {
		t.Fatalf("expected version in usage, got: %s", out)
	}
}
