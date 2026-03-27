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

	if len(os.Args) < 2 {
		cli.PrintUsage()
		os.Exit(1)
	}

	// Dispatch to the appropriate CLI subcommand. "doctor" and "env" are
	// aliases that both run the prerequisite/config check.
	switch os.Args[1] {
	case "doctor", "env":
		cli.RunDoctor(configDir)
	case "exec":
		cli.RunExec(configDir, os.Args[2:])
	case "run":
		cli.RunServer(configDir, os.Args[2:], uiFiles, docsFiles)
	case "status":
		cli.RunStatus(configDir, os.Args[2:])
	case "-help", "--help", "-h":
		cli.PrintUsage()
	default:
		fmt.Fprintf(os.Stderr, "wallfacer: unknown command %q\n\n", os.Args[1])
		cli.PrintUsage()
		os.Exit(1)
	}
}
