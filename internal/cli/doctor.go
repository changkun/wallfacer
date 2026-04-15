package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"changkun.de/x/wallfacer/internal/envconfig"
	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
)

// RunDoctor implements the `wallfacer doctor` subcommand.
// It displays configuration paths, checks prerequisites, and reports
// whether credentials, container runtime, sandbox images, and git are
// ready. Items marked [!] need attention; [ ] are optional.
func RunDoctor(configDir string) {
	v := Version
	if v == "" {
		v = "dev"
	}
	fmt.Printf("wallfacer doctor (%s)\n\n", v)

	issues := 0
	envFile := envOrDefault("ENV_FILE", filepath.Join(configDir, ".env"))

	// --- Paths ---
	fmt.Printf("Config directory:  %s\n", configDir)
	fmt.Printf("Data directory:    %s\n", envOrDefault("DATA_DIR", filepath.Join(configDir, "data")))
	fmt.Printf("Env file:          %s\n", envFile)
	fmt.Printf("Prompts dir:       %s\n", filepath.Join(configDir, "prompts"))
	fmt.Printf("Container command: %s\n", envOrDefault("CONTAINER_CMD", detectContainerRuntime()))
	fmt.Printf("Sandbox image:     %s\n", envOrDefault("SANDBOX_IMAGE", defaultSandboxImage()))
	fmt.Println()

	// --- Config directory ---
	if info, err := os.Stat(configDir); err != nil {
		fmt.Printf("[!] Config directory missing: %s\n", configDir)
		fmt.Printf("    Run 'wallfacer run' once to auto-create it.\n")
		issues++
	} else if !info.IsDir() {
		fmt.Printf("[!] %s exists but is not a directory\n", configDir)
		issues++
	} else {
		fmt.Printf("[ok] Config directory exists\n")
	}

	// --- .env file ---
	raw, err := os.ReadFile(envFile)
	if err != nil {
		fmt.Printf("[!] Env file not found: %s\n", envFile)
		fmt.Printf("    Run 'wallfacer run' once to auto-create it.\n")
		issues++
	} else {
		fmt.Printf("[ok] Env file exists\n")
	}

	// --- Parse env values ---
	vals := map[string]string{}
	if raw != nil {
		for line := range strings.SplitSeq(string(raw), "\n") {
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
	}

	// --- Claude Code sandbox credentials ---
	fmt.Println()
	fmt.Println("Claude Code sandbox:")
	oauthToken := vals["CLAUDE_CODE_OAUTH_TOKEN"]
	apiKey := vals["ANTHROPIC_API_KEY"]
	switch {
	case oauthToken != "" && oauthToken != "your-oauth-token-here":
		fmt.Printf("[ok] CLAUDE_CODE_OAUTH_TOKEN is set (%s)\n", envconfig.MaskToken(oauthToken))
	case apiKey != "":
		fmt.Printf("[ok] ANTHROPIC_API_KEY is set (%s)\n", envconfig.MaskToken(apiKey))
	default:
		fmt.Printf("[!] No Claude credential (CLAUDE_CODE_OAUTH_TOKEN or ANTHROPIC_API_KEY)\n")
		fmt.Printf("    Set one in Settings → API Configuration.\n")
		issues++
	}
	printOptionalVar(vals, "ANTHROPIC_BASE_URL", "using default")
	printOptionalVar(vals, "CLAUDE_DEFAULT_MODEL", "using Claude Code default")
	printOptionalVar(vals, "CLAUDE_TITLE_MODEL", "falls back to default model")

	// --- OpenAI Codex sandbox credentials ---
	fmt.Println()
	fmt.Println("OpenAI Codex sandbox:")
	if openAIKey := vals["OPENAI_API_KEY"]; openAIKey != "" {
		fmt.Printf("[ok] OPENAI_API_KEY is set (%s)\n", envconfig.MaskToken(openAIKey))
	} else {
		fmt.Printf("[ ] OPENAI_API_KEY not set\n")
	}
	printOptionalVar(vals, "OPENAI_BASE_URL", "using OpenAI default")
	printOptionalVar(vals, "CODEX_DEFAULT_MODEL", "using Codex default")
	printOptionalVar(vals, "CODEX_TITLE_MODEL", "falls back to CODEX_DEFAULT_MODEL")

	// --- Container runtime ---
	fmt.Println()
	containerCmd := envOrDefault("CONTAINER_CMD", detectContainerRuntime())
	runtimePath, lookErr := exec.LookPath(containerCmd)
	if lookErr != nil {
		fmt.Printf("[!] Container runtime not found: %s\n", containerCmd)
		fmt.Printf("    Install Podman (https://podman.io) or Docker (https://docker.com).\n")
		issues++
	} else {
		out, err := cmdexec.New(runtimePath, "version", "--format", "{{.Client.Version}}").Output()
		if err != nil {
			fmt.Printf("[!] Container runtime found (%s) but not responding\n", runtimePath)
			if strings.Contains(containerCmd, "podman") {
				fmt.Printf("    Ensure Podman machine is running: podman machine start\n")
			} else {
				fmt.Printf("    Ensure Docker Desktop is running.\n")
			}
			issues++
		} else {
			fmt.Printf("[ok] Container runtime: %s (v%s)\n", runtimePath, out)
		}
	}

	// --- Sandbox backend ---
	sandboxBackend := vals["WALLFACER_SANDBOX_BACKEND"]
	if sandboxBackend == "" {
		sandboxBackend = "local"
	}
	fmt.Printf("[ok] Sandbox backend: %s\n", sandboxBackend)

	// --- Sandbox image ---
	// The unified sandbox-agents image ships both Claude Code and Codex;
	// the entrypoint dispatches via WALLFACER_AGENT. A single image check
	// covers both agent types.
	if lookErr == nil {
		image := envOrDefault("SANDBOX_IMAGE", defaultSandboxImage())
		switch {
		case imageExists(runtimePath, image):
			fmt.Printf("[ok] Sandbox image: %s\n", image)
		case image != fallbackSandboxImage && imageExists(runtimePath, fallbackSandboxImage):
			fmt.Printf("[ ] Sandbox image %s not cached (fallback %s available)\n", image, fallbackSandboxImage)
		default:
			fmt.Printf("[ ] Sandbox image not cached (will be pulled on first task)\n")
		}
	}

	// --- Git ---
	fmt.Println()
	if gitPath, err := exec.LookPath("git"); err != nil {
		fmt.Printf("[!] Git not found\n")
		fmt.Printf("    Git is needed for worktrees, diffs, and auto-push.\n")
		issues++
	} else {
		out, _ := cmdexec.New(gitPath, "--version").Output()
		fmt.Printf("[ok] %s\n", out)
	}

	// --- Summary ---
	fmt.Println()
	if issues == 0 {
		fmt.Printf("All checks passed. Ready to run.\n")
	} else {
		fmt.Printf("%d issue(s) found. Fix the items marked [!] above.\n", issues)
	}
}

// printOptionalVar prints the value of an optional env variable or a
// "not set" note with the given fallback description.
func printOptionalVar(vals map[string]string, key, fallback string) {
	if v := vals[key]; v != "" {
		fmt.Printf("[ok] %s = %s\n", key, v)
	} else {
		fmt.Printf("[ ] %s not set (%s)\n", key, fallback)
	}
}

// imageExists reports whether a container image is available locally.
func imageExists(containerCmd, image string) bool {
	out, err := cmdexec.New(containerCmd, "images", "-q", image).Output()
	return err == nil && out != ""
}
