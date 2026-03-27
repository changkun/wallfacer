//go:build !windows

package cli

import (
	"os"
	"syscall"
)

// execReplace replaces the current process with the given binary via execve(2).
// On Unix this is a true process replacement: the current process image is
// discarded, so the container runtime inherits the terminal (PTY, signals,
// window-resize) directly.
func execReplace(binary string, args []string) error {
	return syscall.Exec(binary, args, os.Environ())
}
