//go:build !windows

package cli

import (
	"os"
	"syscall"
)

var shutdownSignals = []os.Signal{syscall.SIGTERM, os.Interrupt}
