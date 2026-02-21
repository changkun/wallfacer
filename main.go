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
	fmt.Printf("Container command: %s\n", envOrDefault("CONTAINER_CMD", "/opt/podman/bin/podman"))
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

	tokenSet := false
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "CLAUDE_CODE_OAUTH_TOKEN" {
			if v == "" || v == "your-oauth-token-here" {
				fmt.Printf("[!] CLAUDE_CODE_OAUTH_TOKEN is not set — edit %s\n", envFile)
			} else {
				fmt.Printf("[ok] CLAUDE_CODE_OAUTH_TOKEN is set (%s...%s)\n", v[:4], v[len(v)-4:])
				tokenSet = true
			}
		}
	}
	if !tokenSet {
		fmt.Printf("[!] CLAUDE_CODE_OAUTH_TOKEN not found in %s\n", envFile)
	}

	containerCmd := envOrDefault("CONTAINER_CMD", "/opt/podman/bin/podman")
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
		content := "CLAUDE_CODE_OAUTH_TOKEN=your-oauth-token-here\n"
		if err := os.WriteFile(envFile, []byte(content), 0600); err != nil {
			logger.Fatal(logger.Main, "create env file", "error", err)
		}
		logger.Main.Info("created env file — edit it and set your CLAUDE_CODE_OAUTH_TOKEN", "path", envFile)
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
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

