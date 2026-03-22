//go:build windows

package cli

import "os"

var shutdownSignals = []os.Signal{os.Interrupt}
