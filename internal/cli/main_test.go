package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPrintUsage(t *testing.T) {
	out := captureStderr(func() {
		printUsage()
	})
	for _, want := range []string{"Usage: wallfacer <command> [arguments]", "Commands:", "run          start the task board server"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected usage output to contain %q, got: %s", want, out)
		}
	}
}

func TestEnvOrDefault(t *testing.T) {
	t.Setenv("WALLF_TEST_KEY", "value")
	if got := envOrDefault("WALLF_TEST_KEY", "fallback"); got != "value" {
		t.Fatalf("envOrDefault with env set = %q, want value", got)
	}
	t.Setenv("WALLF_TEST_KEY", "")
	if got := envOrDefault("WALLF_TEST_KEY", "fallback"); got != "fallback" {
		t.Fatalf("envOrDefault without env = %q, want fallback", got)
	}
}

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

func TestDetectContainerRuntime(t *testing.T) {
	if _, err := os.Stat("/opt/podman/bin/podman"); err == nil {
		if got := detectContainerRuntime(); got != "/opt/podman/bin/podman" {
			t.Fatalf("expected /opt/podman/bin/podman when available, got %q", got)
		}
		return
	}

	want := "/opt/podman/bin/podman"
	if p, err := exec.LookPath("podman"); err == nil {
		want = p
	} else if p, err := exec.LookPath("docker"); err == nil {
		want = p
	}

	got := detectContainerRuntime()
	if got != want {
		t.Fatalf("detectContainerRuntime() = %q, want %q", got, want)
	}
}

func TestRunEnvCheck_MissingEnvFile(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	t.Setenv("ENV_FILE", envFile)
	t.Setenv("CONTAINER_CMD", "printf")
	t.Setenv("SANDBOX_IMAGE", "wallfacer-test:latest")

	out := captureStdout(func() {
		runEnvCheck(configDir)
	})
	for _, want := range []string{"Config directory:  " + configDir, "Env file:          " + envFile, "[!] Env file not found"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got: %s", want, out)
		}
	}
}

func TestRunEnvCheck_WithCredentials(t *testing.T) {
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
		runEnvCheck(configDir)
	})
	for _, want := range []string{"[ok] CLAUDE_CODE_OAUTH_TOKEN is set", "[ok] OPENAI_API_KEY is set", "[ok] ANTHROPIC_BASE_URL = https://api.anthropic.com", "[ok] OPENAI_BASE_URL = https://api.openai.com/v1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got: %s", want, out)
		}
	}
}

func TestRunEnvCheck_ConfigDirMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-dir")
	envFile := filepath.Join(missing, ".env")
	t.Setenv("ENV_FILE", envFile)
	t.Setenv("CONTAINER_CMD", "printf")

	out := captureStdout(func() {
		runEnvCheck(missing)
	})
	expected := "[!] Config directory does not exist"
	if !strings.Contains(out, expected) {
		t.Fatalf("expected output to contain %q, got: %s", expected, out)
	}
	if !strings.Contains(out, "[!] Env file not found") {
		t.Fatalf("expected env-file warning, got: %s", out)
	}
}

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

func TestRunStatusJsonOutput(t *testing.T) {
	response := `[{"id":"12345678-1234-1234-1234-1234567890ab","title":"test","status":"done","turns":1,"usage":{"cost_usd":0.2}}]`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tasks":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(response))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	out := captureStdout(func() {
		runStatus("", []string{"-addr", ts.URL, "--json"})
	})
	if !strings.Contains(out, response) {
		t.Fatalf("expected raw JSON output, got: %s", out)
	}
}
