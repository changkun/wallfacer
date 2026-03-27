//go:build windows

package cli

import (
	"os"
	"os/exec"
)

// execReplace emulates Unix execve on Windows by spawning the binary as a
// child process with inherited stdio and propagating its exit code. Windows
// does not support true process replacement, so this is the closest equivalent.
func execReplace(binary string, args []string) error {
	cmd := exec.Command(binary, args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	os.Exit(0)
	return nil // unreachable
}
