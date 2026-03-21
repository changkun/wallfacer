package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RunEnvCheck implements the `wallfacer env` subcommand.
func RunEnvCheck(configDir string) {
	envFile := envOrDefault("ENV_FILE", filepath.Join(configDir, ".env"))

	fmt.Printf("Config directory:  %s\n", configDir)
	fmt.Printf("Data directory:    %s\n", envOrDefault("DATA_DIR", filepath.Join(configDir, "data")))
	fmt.Printf("Env file:          %s\n", envFile)
	fmt.Printf("Prompts dir:       %s\n", filepath.Join(configDir, "prompts"))
	fmt.Printf("Container command: %s\n", envOrDefault("CONTAINER_CMD", detectContainerRuntime()))
	fmt.Printf("Sandbox image:     %s\n", envOrDefault("SANDBOX_IMAGE", defaultSandboxImage()))
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

		image := envOrDefault("SANDBOX_IMAGE", defaultSandboxImage())
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
				fmt.Printf("    %s pull %s\n", containerCmd, defaultSandboxImage())
			}
		} else {
			fmt.Printf("[ok] Sandbox image found: %s\n", image)
		}
	}
}
