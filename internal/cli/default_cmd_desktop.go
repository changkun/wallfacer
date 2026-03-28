//go:build desktop

package cli

// DefaultSubcommand returns "desktop" when the desktop build tag is active,
// so launching the binary without arguments (e.g., from Finder or Explorer)
// starts the desktop app.
func DefaultSubcommand() string {
	return "desktop"
}
