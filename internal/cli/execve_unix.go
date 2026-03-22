//go:build !windows

package cli

import (
	"os"
	"syscall"
)

func execReplace(binary string, args []string) error {
	return syscall.Exec(binary, args, os.Environ())
}
