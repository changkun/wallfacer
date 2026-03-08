package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func runExec(_ string, args []string) {
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

	if len(positional) < 1 {
		fmt.Fprintf(os.Stderr, "usage: wallfacer exec <task-id-prefix> [-- command...]\n")
		fmt.Fprintf(os.Stderr, "  <task-id-prefix>  first 8+ hex characters of the task UUID\n")
		fmt.Fprintf(os.Stderr, "  --                separator between the prefix and the in-container command\n")
		fmt.Fprintf(os.Stderr, "  command           command to run inside the container (default: bash)\n")
		os.Exit(1)
	}
	prefix := positional[0]

	if len(prefix) < 8 {
		fmt.Fprintf(os.Stderr, "wallfacer exec: task ID prefix must be at least 8 characters (got %q)\n", prefix)
		os.Exit(1)
	}

	// Detect the container runtime (mirrors the order in internal/runner).
	runtimePath := detectContainerRuntime()

	// List running containers by name.
	out, err := exec.Command(runtimePath, "ps", "--format", "{{.Names}}").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "wallfacer exec: failed to list containers: %v\n", err)
		os.Exit(1)
	}

	containerName, err := resolveContainerByPrefix(string(out), prefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wallfacer exec: %v\n", err)
		os.Exit(1)
	}

	// Replace the current process with the container exec so the terminal
	// (PTY, signals, window-resize) is fully inherited.
	runtimeBin := filepath.Base(runtimePath)
	execArgs := append([]string{runtimeBin, "exec", "-it", containerName}, command...)

	if err := syscall.Exec(runtimePath, execArgs, os.Environ()); err != nil {
		// syscall.Exec only fails when the OS cannot exec the binary itself
		// (e.g. ENOENT or EACCES on runtimePath). If the default "bash" was
		// used, retry with "sh" before giving up — some minimal images only
		// ship sh.
		if len(command) == 1 && command[0] == "bash" {
			shArgs := append([]string{runtimeBin, "exec", "-it", containerName}, "sh")
			if err2 := syscall.Exec(runtimePath, shArgs, os.Environ()); err2 != nil {
				fmt.Fprintf(os.Stderr, "wallfacer exec: %v\n", err2)
				os.Exit(1)
			}
		}
		fmt.Fprintf(os.Stderr, "wallfacer exec: %v\n", err)
		os.Exit(1)
	}
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
