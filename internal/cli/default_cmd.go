//go:build !desktop

package cli

import (
	"os"
)

// DefaultSubcommand returns the subcommand to use when none is provided.
// Without the desktop build tag, there is no default — print usage and exit.
func DefaultSubcommand() string {
	PrintUsage()
	os.Exit(1)
	return "" // unreachable
}
