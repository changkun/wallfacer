package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"changkun.de/wallfacer/internal/logger"
)

// defaultSandboxImage is the published container image pulled automatically
// when the image is not already present locally.
const defaultSandboxImage = "ghcr.io/changkun/wallfacer:latest"

// fallbackSandboxImage is used when the remote image cannot be pulled.
const fallbackSandboxImage = "wallfacer:latest"

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: wallfacer <command> [arguments]\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  run          start the Kanban server\n")
	fmt.Fprintf(os.Stderr, "  env          show configuration and env file status\n")
	fmt.Fprintf(os.Stderr, "  exec         open a shell in a running task container\n")
	fmt.Fprintf(os.Stderr, "\nThe exec subcommand attaches to a task container by its task UUID prefix:\n")
	fmt.Fprintf(os.Stderr, "  wallfacer exec <task-id-prefix> [-- command...]\n")
	fmt.Fprintf(os.Stderr, "  <task-id-prefix>  first 8+ hex characters of the task UUID\n")
	fmt.Fprintf(os.Stderr, "                    (the UUID prefix shown on Kanban UI task cards)\n")
	fmt.Fprintf(os.Stderr, "  command defaults to bash; use '-- sh' if bash is not available.\n")
	fmt.Fprintf(os.Stderr, "\nRun 'wallfacer <command> -help' for more information on a command.\n")
}

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		logger.Fatal(logger.Main, "home dir", "error", err)
	}
	configDir := filepath.Join(home, ".wallfacer")

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "env":
		runEnvCheck(configDir)
	case "exec":
		runExec(configDir, os.Args[2:])
	case "run":
		runServer(configDir, os.Args[2:])
	case "-help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "wallfacer: unknown command %q\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func runEnvCheck(configDir string) {
	envFile := envOrDefault("ENV_FILE", filepath.Join(configDir, ".env"))

	fmt.Printf("Config directory:  %s\n", configDir)
	fmt.Printf("Data directory:    %s\n", envOrDefault("DATA_DIR", filepath.Join(configDir, "data")))
	fmt.Printf("Env file:          %s\n", envFile)
	fmt.Printf("Container command: %s\n", envOrDefault("CONTAINER_CMD", detectContainerRuntime()))
	fmt.Printf("Sandbox image:     %s\n", envOrDefault("SANDBOX_IMAGE", defaultSandboxImage))
	fmt.Println()

	if info, err := os.Stat(configDir); err != nil {
		fmt.Printf("[!] Config directory does not exist (run 'wallfacer run' to auto-create)\n")
	} else if !info.IsDir() {
		fmt.Printf("[!] %s is not a directory\n", configDir)
	} else {
		fmt.Printf("[ok] Config directory exists\n")
	}

	raw, err := os.ReadFile(envFile)
	if err != nil {
		fmt.Printf("[!] Env file not found: %s\n", envFile)
		fmt.Printf("    Run 'wallfacer run' once to auto-create a template, then set your token.\n")
		return
	}
	fmt.Printf("[ok] Env file exists\n")

	vals := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		vals[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}

	// --- Claude Code sandbox ---
	fmt.Println()
	fmt.Println("Claude Code sandbox:")
	oauthToken := vals["CLAUDE_CODE_OAUTH_TOKEN"]
	apiKey := vals["ANTHROPIC_API_KEY"]
	switch {
	case oauthToken != "" && oauthToken != "your-oauth-token-here":
		masked := oauthToken[:4] + "..." + oauthToken[len(oauthToken)-4:]
		if len(oauthToken) <= 8 {
			masked = strings.Repeat("*", len(oauthToken))
		}
		fmt.Printf("[ok] CLAUDE_CODE_OAUTH_TOKEN is set (%s)\n", masked)
	case apiKey != "":
		masked := apiKey[:4] + "..." + apiKey[len(apiKey)-4:]
		if len(apiKey) <= 8 {
			masked = strings.Repeat("*", len(apiKey))
		}
		fmt.Printf("[ok] ANTHROPIC_API_KEY is set (%s)\n", masked)
	default:
		fmt.Printf("[ ] No Claude token set (CLAUDE_CODE_OAUTH_TOKEN or ANTHROPIC_API_KEY)\n")
	}
	if v := vals["ANTHROPIC_BASE_URL"]; v != "" {
		fmt.Printf("[ok] ANTHROPIC_BASE_URL = %s\n", v)
	} else {
		fmt.Printf("[ ] ANTHROPIC_BASE_URL not set (using default)\n")
	}
	if v := vals["CLAUDE_DEFAULT_MODEL"]; v != "" {
		fmt.Printf("[ok] CLAUDE_DEFAULT_MODEL = %s\n", v)
	} else {
		fmt.Printf("[ ] CLAUDE_DEFAULT_MODEL not set (using Claude Code default)\n")
	}
	if v := vals["CLAUDE_TITLE_MODEL"]; v != "" {
		fmt.Printf("[ok] CLAUDE_TITLE_MODEL = %s\n", v)
	} else {
		fmt.Printf("[ ] CLAUDE_TITLE_MODEL not set (falls back to default model)\n")
	}

	// --- OpenAI Codex sandbox ---
	fmt.Println()
	fmt.Println("OpenAI Codex sandbox:")
	openAIKey := vals["OPENAI_API_KEY"]
	if openAIKey != "" {
		masked := openAIKey[:4] + "..." + openAIKey[len(openAIKey)-4:]
		if len(openAIKey) <= 8 {
			masked = strings.Repeat("*", len(openAIKey))
		}
		fmt.Printf("[ok] OPENAI_API_KEY is set (%s)\n", masked)
	} else {
		fmt.Printf("[ ] OPENAI_API_KEY not set\n")
	}
	if v := vals["OPENAI_BASE_URL"]; v != "" {
		fmt.Printf("[ok] OPENAI_BASE_URL = %s\n", v)
	} else {
		fmt.Printf("[ ] OPENAI_BASE_URL not set (using OpenAI default)\n")
	}
	if v := vals["CODEX_DEFAULT_MODEL"]; v != "" {
		fmt.Printf("[ok] CODEX_DEFAULT_MODEL = %s\n", v)
	} else {
		fmt.Printf("[ ] CODEX_DEFAULT_MODEL not set (using Codex default)\n")
	}
	if v := vals["CODEX_TITLE_MODEL"]; v != "" {
		fmt.Printf("[ok] CODEX_TITLE_MODEL = %s\n", v)
	} else {
		fmt.Printf("[ ] CODEX_TITLE_MODEL not set (falls back to CODEX_DEFAULT_MODEL)\n")
	}
	fmt.Println()

	containerCmd := envOrDefault("CONTAINER_CMD", detectContainerRuntime())
	if _, err := exec.LookPath(containerCmd); err != nil {
		fmt.Printf("[!] Container runtime not found: %s\n", containerCmd)
	} else {
		fmt.Printf("[ok] Container runtime found: %s\n", containerCmd)

		image := envOrDefault("SANDBOX_IMAGE", defaultSandboxImage)
		out, err := exec.Command(containerCmd, "images", "-q", image).Output()
		if err != nil || strings.TrimSpace(string(out)) == "" {
			fmt.Printf("[!] Sandbox image not found locally: %s\n", image)
			// Check for the local fallback image.
			if image != fallbackSandboxImage {
				fbOut, fbErr := exec.Command(containerCmd, "images", "-q", fallbackSandboxImage).Output()
				if fbErr == nil && strings.TrimSpace(string(fbOut)) != "" {
					fmt.Printf("[ok] Local fallback image available: %s\n", fallbackSandboxImage)
				} else {
					fmt.Printf("    Run 'wallfacer run' to pull it automatically, or manually:\n")
					fmt.Printf("    %s pull %s\n", containerCmd, image)
				}
			} else {
				fmt.Printf("    Run 'make build' to build it, or manually:\n")
				fmt.Printf("    %s pull %s\n", containerCmd, defaultSandboxImage)
			}
		} else {
			fmt.Printf("[ok] Sandbox image found: %s\n", image)
		}
	}
}

func initConfigDir(configDir, envFile string) {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		logger.Fatal(logger.Main, "create config dir", "error", err)
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
			"# CODEX_TITLE_MODEL=codex-mini-latest\n"
		if err := os.WriteFile(envFile, []byte(content), 0600); err != nil {
			logger.Fatal(logger.Main, "create env file", "error", err)
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
	exec.Command(cmd, url).Start()
}
