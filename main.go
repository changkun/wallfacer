// Package main is the entry point for the wallfacer CLI.
package main

import (
	"embed"
	"fmt"
	"os"

	"latere.ai/x/wallfacer/internal/cli"
)

//go:embed all:frontend/dist
var vueDist embed.FS // vueDist holds the Vue SPA dist served by the web server.

//go:embed docs
var docsFiles embed.FS // docsFiles holds the user-facing documentation served at /docs.

func main() {
	configDir := cli.ConfigDir()

	// Determine the subcommand. When no arguments are given, print usage.
	if len(os.Args) < 2 {
		cli.PrintUsage()
		os.Exit(1)
	}
	subcmd := os.Args[1]
	// Remaining args after the subcommand.
	var args []string
	if len(os.Args) > 2 {
		args = os.Args[2:]
	}

	// Dispatch to the appropriate CLI subcommand. "doctor" and "env" are
	// aliases that both run the prerequisite/config check.
	switch subcmd {
	case "doctor", "env":
		cli.RunDoctor(configDir, args)
	case "run":
		cli.RunServer(configDir, args, vueDist, docsFiles)
	case "status":
		cli.RunStatus(configDir, args)
	case "spec":
		cli.RunSpec(configDir, args)
	case "auth":
		cli.RunAuth(configDir, args)
	case "web":
		cli.RunWeb(args)
	case "-help", "--help", "-h":
		cli.PrintUsage()
	default:
		fmt.Fprintf(os.Stderr, "wallfacer: unknown command %q\n\n", os.Args[1])
		cli.PrintUsage()
		os.Exit(1)
	}
}
