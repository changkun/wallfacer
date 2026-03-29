// Package main is the entry point for the wallfacer CLI.
package main

import (
	"embed"
	"fmt"
	"os"

	"changkun.de/x/wallfacer/internal/cli"
)

//go:embed ui
var uiFiles embed.FS // uiFiles holds the frontend assets (HTML, JS, CSS) served by the web server.

//go:embed docs
var docsFiles embed.FS // docsFiles holds the user-facing documentation served at /docs.

func main() {
	configDir := cli.ConfigDir()

	// Determine the subcommand. When no arguments are given (e.g., launched
	// from Finder/desktop), default to "desktop" if the desktop build tag is
	// active, otherwise print usage.
	subcmd := ""
	if len(os.Args) >= 2 {
		subcmd = os.Args[1]
	}
	// macOS passes -psn_<pid> (process serial number) when launching a .app
	// bundle from Finder. Strip any dash-prefixed arg that isn't a help flag
	// so the app falls through to the default subcommand (desktop mode).
	if len(subcmd) > 0 && subcmd[0] == '-' && subcmd != "-help" && subcmd != "--help" && subcmd != "-h" {
		subcmd = ""
	}
	// Remaining args after the subcommand (empty when launched without args).
	var args []string
	if subcmd == "" {
		subcmd = cli.DefaultSubcommand()
	} else if len(os.Args) > 2 {
		args = os.Args[2:]
	}

	// Dispatch to the appropriate CLI subcommand. "doctor" and "env" are
	// aliases that both run the prerequisite/config check.
	switch subcmd {
	case "doctor", "env":
		cli.RunDoctor(configDir)
	case "exec":
		cli.RunExec(configDir, args)
	case "run":
		cli.RunServer(configDir, args, uiFiles, docsFiles)
	case "desktop":
		if err := cli.RunDesktop(configDir, args, uiFiles, docsFiles); err != nil {
			fmt.Fprintln(os.Stderr, "wallfacer:", err)
			os.Exit(1)
		}
	case "status":
		cli.RunStatus(configDir, args)
	case "-help", "--help", "-h":
		cli.PrintUsage()
	default:
		fmt.Fprintf(os.Stderr, "wallfacer: unknown command %q\n\n", os.Args[1])
		cli.PrintUsage()
		os.Exit(1)
	}
}
