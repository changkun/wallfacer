// Package cli implements the wallfacer CLI subcommands.
package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"changkun.de/x/wallfacer/internal/logger"
)

// Version is set at build time via -ldflags. When empty (dev build),
// the binary pulls the :latest sandbox image.
var Version = ""

// sandboxImageBase is the registry path for the published sandbox image.
const sandboxImageBase = "ghcr.io/changkun/wallfacer"

// defaultSandboxImage returns the tagged sandbox image reference.
// Release builds (version set via ldflags) pull the matching version tag;
// dev builds fall back to :latest.
func defaultSandboxImage() string {
	if Version != "" {
		return sandboxImageBase + ":" + Version
	}
	return sandboxImageBase + ":latest"
}

// fallbackSandboxImage is used when the remote image cannot be pulled.
const fallbackSandboxImage = "wallfacer:latest"

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
	fmt.Fprintf(os.Stderr, "  status       print running board state to terminal\n")
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
			"# OpenAI Codex sandbox (use with wallfacer-codex image)\n" +
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

	// Preferred: explicit podman installation.
	if _, err := os.Stat("/opt/podman/bin/podman"); err == nil {
		return "/opt/podman/bin/podman"
	}
	// Fallback: podman on $PATH.
	if p, err := exec.LookPath("podman"); err == nil {
		return p
	}
	// Fallback: docker on $PATH.
	if p, err := exec.LookPath("docker"); err == nil {
		return p
	}
	// Nothing found; return the traditional default so the error message is clear.
	return "/opt/podman/bin/podman"
}

func openBrowser(url string) {
	var cmd string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "linux":
		cmd = "xdg-open"
	default:
		return
	}
	_ = exec.Command(cmd, url).Start()
}
