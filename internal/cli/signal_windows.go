//go:build windows

package cli

import "os"

// shutdownSignals lists the OS signals that trigger a graceful server shutdown.
// On Windows, only SIGINT (Ctrl-C) is supported; SIGTERM is not available.
var shutdownSignals = []os.Signal{os.Interrupt}
