//go:build !windows

package cli

import (
	"os"
	"syscall"
)

// shutdownSignals lists the OS signals that trigger a graceful server shutdown.
// On Unix, both SIGTERM (sent by container runtimes) and SIGINT (Ctrl-C) are handled.
var shutdownSignals = []os.Signal{syscall.SIGTERM, os.Interrupt}
