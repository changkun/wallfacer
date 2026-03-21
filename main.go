// Package main is the entry point for the wallfacer CLI.
package main

import (
	"embed"
	"fmt"
	"os"

	"changkun.de/x/wallfacer/internal/cli"
)

//go:embed ui
var uiFiles embed.FS

//go:embed docs
var docsFiles embed.FS

func main() {
	configDir := cli.ConfigDir()

	if len(os.Args) < 2 {
		cli.PrintUsage()
		os.Exit(1)
	}

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
