package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"changkun.de/wallfacer/internal/sandbox"
)

type execMode int

const (
	execModeTask execMode = iota
	execModeSandbox
)

type execConfig struct {
	mode    execMode
	prefix  string
	sandbox sandbox.Type
	command []string
}

func runExec(configDir string, args []string) {
	// Split args on "--": everything before is positional, after is the command.
	sepIdx := -1
	for i, a := range args {
		if a == "--" {
			sepIdx = i
			break
		}
	}

	var positional []string
	command := []string{"bash"} // default shell inside the container
	if sepIdx >= 0 {
		positional = args[:sepIdx]
		if sepIdx+1 < len(args) {
			command = args[sepIdx+1:]
		}
	} else {
		positional = args
	}

	cfg, err := parseExecConfig(positional, command)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wallfacer exec: %v\n", err)
		fmt.Fprintf(os.Stderr, "usage: wallfacer exec <task-id-prefix> [-- command...]\n")
		fmt.Fprintf(os.Stderr, "   or: wallfacer exec --sandbox <claude|codex> [-- command...]\n")
		fmt.Fprintf(os.Stderr, "  <task-id-prefix>  first 8+ hex characters of the task UUID\n")
		fmt.Fprintf(os.Stderr, "  --sandbox         run an interactive shell in a sandbox image directly\n")
		fmt.Fprintf(os.Stderr, "  --                separator between the prefix and the in-container command\n")
		fmt.Fprintf(os.Stderr, "  command           command to run inside the container (default: bash)\n")
		os.Exit(1)
	}

	// Detect the container runtime (mirrors the order in internal/runner).
	runtimePath := detectContainerRuntime()
	runtimeBin := filepath.Base(runtimePath)

	var execArgs []string
	switch cfg.mode {
	case execModeSandbox:
		execArgs, err = buildSandboxExecArgs(runtimePath, configDir, cfg.sandbox, cfg.command)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wallfacer exec: %v\n", err)
			os.Exit(1)
		}
	default:
		// List running containers by name.
		out, err := exec.Command(runtimePath, "ps", "--format", "{{.Names}}").Output()
		if err != nil {
			fmt.Fprintf(os.Stderr, "wallfacer exec: failed to list containers: %v\n", err)
			os.Exit(1)
		}

		containerName, err := resolveContainerByPrefix(string(out), cfg.prefix)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wallfacer exec: %v\n", err)
			os.Exit(1)
		}
		execArgs = append([]string{runtimeBin, "exec", "-it", containerName}, cfg.command...)
	}

	// Replace the current process with the container exec so the terminal
	// (PTY, signals, window-resize) is fully inherited.
	if err := syscall.Exec(runtimePath, execArgs, os.Environ()); err != nil {
		// syscall.Exec only fails when the OS cannot exec the binary itself
		// (e.g. ENOENT or EACCES on runtimePath). If the default "bash" was
		// used, retry with "sh" before giving up — some minimal images only
		// ship sh.
		if len(cfg.command) == 1 && cfg.command[0] == "bash" {
			shArgs := append(execArgs[:len(execArgs)-1], "sh")
			if err2 := syscall.Exec(runtimePath, shArgs, os.Environ()); err2 != nil {
				fmt.Fprintf(os.Stderr, "wallfacer exec: %v\n", err2)
				os.Exit(1)
			}
		}
		fmt.Fprintf(os.Stderr, "wallfacer exec: %v\n", err)
		os.Exit(1)
	}
}

func parseExecConfig(positional, command []string) (*execConfig, error) {
	cfg := &execConfig{mode: execModeTask, command: command}
	for i := 0; i < len(positional); i++ {
		if positional[i] == "--sandbox" {
			if i+1 >= len(positional) {
				return nil, fmt.Errorf("missing sandbox value for --sandbox")
			}
			cfg.mode = execModeSandbox
			cfg.sandbox = sandbox.Normalize(positional[i+1])
			if i+2 < len(positional) {
				cfg.command = append(cfg.command[:0], positional[i+2:]...)
			}
			positional = positional[:0]
			break
		}
	}

	switch cfg.mode {
	case execModeSandbox:
		if !cfg.sandbox.IsValid() {
			return nil, fmt.Errorf("invalid sandbox %q (use claude or codex)", cfg.sandbox)
		}
		if len(positional) > 0 && len(cfg.command) == 1 && cfg.command[0] == "bash" {
			return nil, fmt.Errorf("task prefix is not allowed in --sandbox mode")
		}
	default:
		if len(positional) < 1 {
			return nil, fmt.Errorf("missing task-id-prefix")
		}
		cfg.prefix = strings.TrimSpace(positional[0])
		if len(cfg.prefix) < 8 {
			return nil, fmt.Errorf("task ID prefix must be at least 8 characters (got %q)", cfg.prefix)
		}
	}

	return cfg, nil
}

func buildSandboxExecArgs(runtimePath, configDir string, sb sandbox.Type, command []string) ([]string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getwd: %w", err)
	}
	base := filepath.Base(cwd)
	if base == "." || base == "/" || base == "" {
		base = "workspace"
	}
	image := resolveSandboxImageForExec(envOrDefault("SANDBOX_IMAGE", defaultSandboxImage), sb)
	envFile := envOrDefault("ENV_FILE", filepath.Join(configDir, ".env"))
	runtimeBin := filepath.Base(runtimePath)

	args := []string{runtimeBin, "run", "--rm", "-it", "--network=host"}
	if info, err := os.Stat(envFile); err == nil && !info.IsDir() {
		args = append(args, "--env-file", envFile)
	}
	if sb == sandbox.Claude {
		args = append(args, "-v", "claude-config:/home/claude/.claude")
	}
	if sb == sandbox.Codex {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			codexPath := filepath.Join(home, ".codex")
			if info, err := os.Stat(filepath.Join(codexPath, "auth.json")); err == nil && !info.IsDir() {
				args = append(args, "-v", codexPath+":/home/codex/.codex:z,ro")
			}
		}
	}
	args = append(args,
		"-v", cwd+":/workspace/"+base+":z",
		"-w", "/workspace/"+base,
		image,
	)
	args = append(args, command...)
	return args, nil
}

func resolveSandboxImageForExec(baseImage string, sb sandbox.Type) string {
	baseImage = strings.TrimSpace(baseImage)
	if sb != sandbox.Codex {
		return baseImage
	}
	if baseImage == "" {
		return "wallfacer-codex:latest"
	}
	low := strings.ToLower(baseImage)
	if strings.Contains(low, "wallfacer-codex") {
		return baseImage
	}

	registry := baseImage
	digest := ""
	if at := strings.Index(registry, "@"); at != -1 {
		digest = registry[at:]
		registry = registry[:at]
	}
	tag := ""
	if at := strings.LastIndex(registry, ":"); at != -1 {
		tag = registry[at:]
		registry = registry[:at]
	}
	prefix := ""
	repoName := registry
	if idx := strings.LastIndex(repoName, "/"); idx != -1 {
		prefix = repoName[:idx+1]
		repoName = repoName[idx+1:]
	}
	if repoName != "wallfacer" {
		return baseImage
	}
	return prefix + "wallfacer-codex" + tag + digest
}

// resolveContainerByPrefix searches the newline-separated output of
// `<runtime> ps --format {{.Names}}` for a container whose name contains
// the given task-ID prefix as a substring. It returns the matching container
// name, or an error if no match or more than one match is found.
func resolveContainerByPrefix(psOutput, prefix string) (string, error) {
	var matches []string
	for _, line := range strings.Split(psOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, prefix) {
			matches = append(matches, line)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no running container for task prefix %q — is the task in_progress?", prefix)
	case 1:
		return matches[0], nil
	default:
		var sb strings.Builder
		fmt.Fprintf(&sb, "multiple containers match prefix %q; be more specific:\n", prefix)
		for _, m := range matches {
			fmt.Fprintf(&sb, "  %s\n", m)
		}
		return "", fmt.Errorf("%s", sb.String())
	}
}
