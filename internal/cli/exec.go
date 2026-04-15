package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
	"changkun.de/x/wallfacer/internal/pkg/sanitize"
	"changkun.de/x/wallfacer/internal/sandbox"
)

// execMode distinguishes between attaching to a running task container
// and launching a fresh sandbox shell.
type execMode int

const (
	execModeTask    execMode = iota // attach to an existing task container by UUID prefix
	execModeSandbox                 // launch a new interactive sandbox container
)

// execConfig holds the parsed arguments for the exec subcommand.
type execConfig struct {
	mode    execMode     // task or sandbox mode
	prefix  string       // task UUID prefix (task mode only)
	sandbox sandbox.Type // sandbox type to launch (sandbox mode only)
	command []string     // command to run inside the container
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
	// (PTY, signals, window-resize) is fully inherited. On Unix this is a
	// true execve(2); on Windows it spawns a child process (see execve_*.go).
	if err := execReplace(runtimePath, execArgs); err != nil {
		// execReplace only fails when the OS cannot exec the binary itself
		// (e.g. ENOENT or EACCES on runtimePath). If the default "bash" was
		// used, retry with "sh" before giving up — some minimal images only
		// ship sh.
		if len(cfg.command) == 1 && cfg.command[0] == "bash" {
			// Three-index slice ensures append allocates a new backing array
			// rather than mutating execArgs in place.
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

// parseExecConfig interprets the positional arguments to determine exec mode
// (task vs sandbox), validates the arguments, and returns the configuration.
//
// Argument grammar:
//
//	<task-id-prefix>                     → task mode
//	--sandbox <claude|codex> [command…]  → sandbox mode
func parseExecConfig(positional, command []string) (*execConfig, error) {
	cfg := &execConfig{mode: execModeTask, command: command}
	for i := 0; i < len(positional); i++ {
		if positional[i] == "--sandbox" {
			if i+1 >= len(positional) {
				return nil, fmt.Errorf("missing sandbox value for --sandbox")
			}
			cfg.mode = execModeSandbox
			cfg.sandbox = sandbox.Normalize(positional[i+1])
			// Any arguments after --sandbox <type> become the in-container command.
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
		// Require at least 8 hex characters to avoid ambiguous matches against
		// the wallfacer-<slug>-<uuid-prefix> container naming convention.
		if len(cfg.prefix) < 8 {
			return nil, fmt.Errorf("task ID prefix must be at least 8 characters (got %q)", cfg.prefix)
		}
	}

	return cfg, nil
}

// buildSandboxExecArgs constructs the container-runtime argument list for
// launching a new interactive sandbox container. It mounts the current working
// directory as a workspace, injects the .env file, and wires up sandbox-specific
// configuration volumes (e.g. claude config, codex auth).
func buildSandboxExecArgs(runtimePath, configDir string, sb sandbox.Type, command []string) ([]string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getwd: %w", err)
	}
	base := sanitize.Base(filepath.Base(cwd))
	image := strings.TrimSpace(envOrDefault("SANDBOX_IMAGE", defaultSandboxImage()))
	envFile := envOrDefault("ENV_FILE", filepath.Join(configDir, ".env"))
	runtimeBin := filepath.Base(runtimePath)

	args := []string{runtimeBin, "run", "--rm", "-it", "--network=host"}
	if info, err := os.Stat(envFile); err == nil && !info.IsDir() {
		args = append(args, "--env-file", envFile)
	}
	// The unified sandbox image dispatches between Claude Code and Codex at
	// runtime via WALLFACER_AGENT. Set it to the requested sandbox so the
	// entrypoint launches the right CLI.
	args = append(args, "-e", "WALLFACER_AGENT="+string(sb))
	if sb == sandbox.Claude {
		args = append(args, "-v", "claude-config:/home/agent/.claude")
	}
	if sb == sandbox.Codex {
		// Mount the host's ~/.codex/auth.json (read-only) into the container
		// so Codex can authenticate without re-login. Mount only the file —
		// Codex 0.120+ writes config.toml and session state into ~/.codex at
		// startup, which requires the directory itself to be writable inside
		// the container.
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			authFile := filepath.Join(home, ".codex", "auth.json")
			if info, err := os.Stat(authFile); err == nil && !info.IsDir() {
				args = append(args, "--mount", "type=bind,src="+authFile+",dst=/home/agent/.codex/auth.json,readonly,z")
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

// resolveContainerByPrefix searches the newline-separated output of
// `<runtime> ps --format {{.Names}}` for a container whose name contains
// the given task-ID prefix as a substring. It returns the matching container
// name, or an error if no match or more than one match is found.
func resolveContainerByPrefix(psOutput, prefix string) (string, error) {
	var matches []string
	for line := range strings.SplitSeq(psOutput, "\n") {
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
