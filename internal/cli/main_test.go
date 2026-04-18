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

// TestEnvOrDefault verifies that envOrDefault returns the env value when set
// and the fallback when the variable is empty.
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

// TestDetectContainerRuntime checks that the detection prefers /opt/podman
// when available and otherwise falls back through podman/docker on $PATH.
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

// TestRunDoctor_ConfigDirMissing verifies that doctor warns when both the
// config directory and .env file are absent.
func TestRunDoctor_ConfigDirMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-dir")
	envFile := filepath.Join(missing, ".env")
	t.Setenv("ENV_FILE", envFile)
	t.Setenv("CONTAINER_CMD", "printf")

	out := captureStdout(func() {
		RunDoctor(missing, nil)
	})
	if !strings.Contains(out, "[!] Config directory missing") {
		t.Fatalf("expected config dir warning, got: %s", out)
	}
	if !strings.Contains(out, "[!] Env file not found") {
		t.Fatalf("expected env-file warning, got: %s", out)
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

// TestDefaultSandboxImage_WithSandboxTag verifies that a build-time SandboxTag
// is used verbatim (already includes the "v" prefix).
func TestDefaultSandboxImage_WithSandboxTag(t *testing.T) {
	oldTag := SandboxTag
	SandboxTag = "v0.0.4"
	defer func() { SandboxTag = oldTag }()

	got := defaultSandboxImage()
	if got != sandboxImageBase+":v0.0.4" {
		t.Fatalf("expected image tag :v0.0.4, got %q", got)
	}
}

// TestDefaultSandboxImage_VersionDoesNotDriveTag verifies that wallfacer's own
// Version must NOT be used as the sandbox image tag. The sandbox image comes
// from github.com/latere-ai/images, which releases independently of wallfacer.
// Setting Version without SandboxTag must fall back to runtime resolution or
// :latest, never to ":v<wallfacer-version>".
func TestDefaultSandboxImage_VersionDoesNotDriveTag(t *testing.T) {
	oldVersion := Version
	oldTag := SandboxTag
	Version = "9.9.9"
	SandboxTag = ""
	defer func() {
		Version = oldVersion
		SandboxTag = oldTag
	}()

	got := defaultSandboxImage()
	if got == sandboxImageBase+":v9.9.9" {
		t.Fatalf("sandbox tag must not be derived from wallfacer Version; got %q", got)
	}
}

// TestDefaultSandboxImage_DevBuild verifies that when no SandboxTag is embedded
// the binary either resolves the latest release tag from latere-ai/images or
// falls back to :latest.
func TestDefaultSandboxImage_DevBuild(t *testing.T) {
	oldVersion := Version
	oldTag := SandboxTag
	Version = ""
	SandboxTag = ""
	defer func() {
		Version = oldVersion
		SandboxTag = oldTag
	}()

	got := defaultSandboxImage()
	// Dev build queries GitHub API for the latest tag; if the query succeeds
	// we get e.g. ":v0.0.4", otherwise ":latest" as fallback.
	if got == sandboxImageBase+":latest" {
		return // fallback path — OK
	}
	if !strings.HasPrefix(got, sandboxImageBase+":") {
		t.Fatalf("unexpected image base, got %q", got)
	}
	tag := strings.TrimPrefix(got, sandboxImageBase+":")
	if tag == "" || tag == "latest" {
		t.Fatalf("expected a resolved tag or :latest, got %q", got)
	}
	// Resolved a real tag (e.g. "v0.0.4") — valid.
}

// TestDetectContainerRuntime_EnvOverride verifies that CONTAINER_CMD env var
// overrides all other detection logic.
func TestDetectContainerRuntime_EnvOverride(t *testing.T) {
	t.Setenv("CONTAINER_CMD", "/custom/runtime")
	got := detectContainerRuntime()
	if got != "/custom/runtime" {
		t.Fatalf("expected /custom/runtime, got %q", got)
	}
}

// TestDetectContainerRuntime_EnvOverrideTrimmed verifies whitespace is trimmed
// from the CONTAINER_CMD override.
func TestDetectContainerRuntime_EnvOverrideTrimmed(t *testing.T) {
	t.Setenv("CONTAINER_CMD", "  /custom/runtime  ")
	got := detectContainerRuntime()
	if got != "/custom/runtime" {
		t.Fatalf("expected trimmed path, got %q", got)
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

// TestRunStatusJsonOutput verifies that `wallfacer status --json` outputs the
// raw JSON response from the server without any formatting.
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
		RunStatus("", []string{"-addr", ts.URL, "--json"})
	})
	if !strings.Contains(out, response) {
		t.Fatalf("expected raw JSON output, got: %s", out)
	}
}
