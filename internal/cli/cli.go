// Package cli implements the wallfacer CLI subcommands.
package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/logger"
)

// Version is set at build time via -ldflags (e.g. -X cli.Version=1.2.3).
// When empty (dev build), the doctor subcommand displays "dev" as the
// version string. Version tracks wallfacer's own release and is
// intentionally independent of the sandbox image tag.
var Version = ""

// SandboxTag is the sandbox image tag embedded at build time via -ldflags
// (e.g. -X cli.SandboxTag=v0.0.4). It is resolved from the latest release of
// github.com/latere-ai/images at build time and MUST NOT be derived from
// wallfacer's own Version — the two repositories release independently.
var SandboxTag = ""

// sandboxImageBase is the registry path for the published sandbox image.
// The single sandbox-agents image ships both Claude Code and Codex CLIs;
// the entrypoint selects between them at runtime via WALLFACER_AGENT.
const sandboxImageBase = "ghcr.io/latere-ai/sandbox-agents"

// defaultSandboxImage returns the tagged sandbox image reference.
// Builds that embed SandboxTag via ldflags use that tag directly.
// Otherwise the binary queries the GitHub API for the latest release of
// latere-ai/images and uses that tag. Falls back to :latest only if the
// query fails.
func defaultSandboxImage() string {
	if SandboxTag != "" {
		return sandboxImageBase + ":" + SandboxTag
	}
	if tag := resolveLatestImageTag(); tag != "" {
		logger.Main.Info("resolved latest sandbox image tag", "tag", tag)
		return sandboxImageBase + ":" + tag
	}
	return sandboxImageBase + ":latest"
}

// resolveLatestImageTag queries the GitHub API for the latest release tag
// of the latere-ai/images repository. Returns the tag name (e.g. "v0.0.3")
// or empty string on failure.
func resolveLatestImageTag() string {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/latere-ai/images/releases/latest")
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return ""
	}
	return release.TagName
}

// fallbackSandboxImage is the locally-built image name used when the
// remote registry image cannot be pulled (e.g. no network, auth failure).
// This intentionally uses :latest as a last-resort local fallback.
const fallbackSandboxImage = "sandbox-agents:latest"

// ConfigDir returns the default wallfacer configuration directory.
func ConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		logger.Fatal("home dir", "error", err)
	}
	return filepath.Join(home, ".wallfacer")
}

// PrintUsage writes the CLI help text to stderr.
func PrintUsage() {
	v := Version
	if v == "" {
		v = "dev"
	}
	fmt.Fprintf(os.Stderr, "wallfacer %s\n\n", v)
	fmt.Fprintf(os.Stderr, "Usage: wallfacer <command> [arguments]\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  run          start the task board server\n")
	fmt.Fprintf(os.Stderr, "  desktop      launch the native desktop app (requires -tags desktop build)\n")
	fmt.Fprintf(os.Stderr, "  status       print running board state to terminal\n")
	fmt.Fprintf(os.Stderr, "  spec         spec document tools (validate, ...)\n")
	fmt.Fprintf(os.Stderr, "  doctor       check prerequisites and configuration\n")
	fmt.Fprintf(os.Stderr, "  exec         open a shell in a running task container\n")
	fmt.Fprintf(os.Stderr, "\nThe exec subcommand attaches to a task container by its task UUID prefix:\n")
	fmt.Fprintf(os.Stderr, "  wallfacer exec <task-id-prefix> [-- command...]\n")
	fmt.Fprintf(os.Stderr, "  wallfacer exec --sandbox <claude|codex> [-- command...]\n")
	fmt.Fprintf(os.Stderr, "  <task-id-prefix>  first 8+ hex characters of the task UUID\n")
	fmt.Fprintf(os.Stderr, "                    (the UUID prefix shown on task board UI task cards)\n")
	fmt.Fprintf(os.Stderr, "  command defaults to bash; use '-- sh' if bash is not available.\n")
	fmt.Fprintf(os.Stderr, "\nRun 'wallfacer <command> -help' for more information on a command.\n")
}

// initConfigDir ensures the configuration directory exists and seeds the .env
// template file if it does not already exist.
func initConfigDir(configDir, envFile string) {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		logger.Fatal("create config dir", "error", err)
	}

	if _, err := os.Stat(envFile); os.IsNotExist(err) {
		content := "# =============================================================================\n" +
			"# Claude Code sandbox (default)\n" +
			"# =============================================================================\n\n" +
			"# Authentication: set ONE of the two variables below.\n" +
			"CLAUDE_CODE_OAUTH_TOKEN=your-oauth-token-here\n" +
			"# ANTHROPIC_API_KEY=sk-ant-...\n\n" +
			"# Optional: custom Anthropic-compatible API base URL.\n" +
			"# ANTHROPIC_BASE_URL=https://api.anthropic.com\n\n" +
			"# Optional: default model for Claude tasks.\n" +
			"# CLAUDE_DEFAULT_MODEL=\n\n" +
			"# Optional: model for auto-generating task titles (falls back to default model).\n" +
			"# CLAUDE_TITLE_MODEL=\n\n" +
			"# =============================================================================\n" +
			"# OpenAI Codex sandbox (uses the unified sandbox-agents image; WALLFACER_AGENT=codex selects this CLI at runtime)\n" +
			"# =============================================================================\n\n" +
			"# Authentication: set your OpenAI API key.\n" +
			"# OPENAI_API_KEY=sk-...\n\n" +
			"# Optional: custom OpenAI-compatible API base URL.\n" +
			"# OPENAI_BASE_URL=https://api.openai.com/v1\n\n" +
			"# Optional: default model for Codex tasks.\n" +
			"# CODEX_DEFAULT_MODEL=codex-mini-latest\n\n" +
			"# Optional: model for auto-generating task titles with Codex (falls back to CODEX_DEFAULT_MODEL).\n" +
			"# CODEX_TITLE_MODEL=codex-mini-latest\n\n" +
			"# Optional: enable fast-mode sandbox hints by default (set to false to disable).\n" +
			"WALLFACER_SANDBOX_FAST=true\n"
		if err := os.WriteFile(envFile, []byte(content), 0600); err != nil {
			logger.Fatal("create env file", "error", err)
		}
		logger.Main.Info("created env file — edit it and set your token", "path", envFile)
	}
}

// envOrDefault returns the value of the environment variable key, or fallback
// if the variable is unset or empty.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// detectContainerRuntime returns the path to the container runtime binary.
// It prefers /opt/podman/bin/podman, then falls back to "podman" and "docker"
// on $PATH. Returns the hardcoded default if nothing is found.
func detectContainerRuntime() string {
	if override := strings.TrimSpace(os.Getenv("CONTAINER_CMD")); override != "" {
		return override
	}

	// Unix: preferred explicit podman installation.
	if runtime.GOOS != "windows" {
		if _, err := os.Stat("/opt/podman/bin/podman"); err == nil {
			return "/opt/podman/bin/podman"
		}
	}
	// Windows: check common install locations.
	if runtime.GOOS == "windows" {
		for _, candidate := range []string{
			filepath.Join(os.Getenv("ProgramFiles"), "RedHat", "Podman", "podman.exe"),
		} {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	// Cross-platform: podman on $PATH.
	if p, err := exec.LookPath("podman"); err == nil {
		return p
	}
	// Cross-platform: docker on $PATH.
	if p, err := exec.LookPath("docker"); err == nil {
		return p
	}
	// Nothing found; return a platform-appropriate default.
	if runtime.GOOS == "windows" {
		return "podman.exe"
	}
	return "/opt/podman/bin/podman"
}

// isWSL reports whether the process is running inside Windows Subsystem for Linux.
func isWSL() bool {
	return os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != ""
}

// openBrowser launches the platform's default browser with the given URL.
// Under WSL, it delegates to cmd.exe so the Windows host browser opens.
func openBrowser(url string) {
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("open", url).Start()
	case "windows":
		_ = exec.Command("cmd", "/c", "start", url).Start()
	case "linux":
		if isWSL() {
			_ = exec.Command("cmd.exe", "/c", "start", url).Start()
		} else {
			_ = exec.Command("xdg-open", url).Start()
		}
	}
}
