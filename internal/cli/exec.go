package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
)

type execMode int

const (
	execModeTask execMode = iota
	execModeSandbox
)

type execConfig struct {
	mode    execMode
	prefix  string
	sandbox constants.SandboxType
	command []string
}

// RunExec attaches to a running task container or opens a new sandbox shell.
func RunExec(configDir string, args []string) {
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
		out, err := cmdexec.New(runtimePath, "ps", "--format", "{{.Names}}").Output()
		if err != nil {
			fmt.Fprintf(os.Stderr, "wallfacer exec: failed to list containers: %v\n", err)
			os.Exit(1)
		}

		containerName, err := resolveContainerByPrefix(out, cfg.prefix)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wallfacer exec: %v\n", err)
			os.Exit(1)
		}
		execArgs = append([]string{runtimeBin, "exec", "-it", containerName}, cfg.command...)
	}

	// Replace the current process with the container exec so the terminal
	// (PTY, signals, window-resize) is fully inherited.
	if err := execReplace(runtimePath, execArgs); err != nil {
		// execReplace only fails when the OS cannot exec the binary itself
		// (e.g. ENOENT or EACCES on runtimePath). If the default "bash" was
		// used, retry with "sh" before giving up — some minimal images only
		// ship sh.
		if len(cfg.command) == 1 && cfg.command[0] == "bash" {
			shArgs := append(execArgs[:len(execArgs)-1:len(execArgs)-1], "sh") //nolint:gocritic // intentionally assigned to new slice
			if err2 := execReplace(runtimePath, shArgs); err2 != nil {
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
			cfg.sandbox = constants.NormalizeSandboxType(positional[i+1])
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

func buildSandboxExecArgs(runtimePath, configDir string, sb constants.SandboxType, command []string) ([]string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getwd: %w", err)
	}
	base := sanitizeContainerBasename(filepath.Base(cwd))
	image := resolveSandboxImageForExec(envOrDefault("SANDBOX_IMAGE", defaultSandboxImage()), sb)
	envFile := envOrDefault("ENV_FILE", filepath.Join(configDir, ".env"))
	runtimeBin := filepath.Base(runtimePath)

	args := []string{runtimeBin, "run", "--rm", "-it", "--network=host"}
	if info, err := os.Stat(envFile); err == nil && !info.IsDir() {
		args = append(args, "--env-file", envFile)
	}
	if sb == constants.SandboxClaude {
		args = append(args, "-v", "claude-config:/home/claude/.claude")
	}
	if sb == constants.SandboxCodex {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			codexPath := filepath.Join(home, ".codex")
			if info, err := os.Stat(filepath.Join(codexPath, "auth.json")); err == nil && !info.IsDir() {
				args = append(args, "--mount", "type=bind,src="+codexPath+",dst=/home/codex/.codex,readonly,z")
			}
		}
	}
	containerPath := "/workspace/" + base
	args = append(args,
		"--mount", "type=bind,src="+cwd+",dst="+containerPath+",z",
		"-w", containerPath,
		image,
	)
	args = append(args, command...)
	return args, nil
}

func resolveSandboxImageForExec(baseImage string, sb constants.SandboxType) string {
	baseImage = strings.TrimSpace(baseImage)
	if sb != constants.SandboxCodex {
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

// sanitizeContainerBasename returns a container-path-safe version of a
// directory basename. Replaces characters problematic in mount paths (colons,
// spaces, shell metacharacters) with underscores, preserving unicode
// letters/digits. Falls back to "workspace" if the result is empty.
func sanitizeContainerBasename(base string) string {
	if base == "." || base == "/" || base == "" {
		return "workspace"
	}
	var b strings.Builder
	for _, r := range base {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	result := b.String()
	if result == "" {
		return "workspace"
	}
	return result
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
