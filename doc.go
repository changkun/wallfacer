// Package main is the entry point for the wallfacer CLI application.
//
// Wallfacer is a task-board runner for AI agents. It provides a web UI where
// tasks are created as cards, dragged to "In Progress" to trigger AI agent
// execution in an isolated sandbox container, and results are inspected when done.
//
// This package embeds the frontend UI assets and documentation filesystem into the
// binary via go:embed, then dispatches to CLI subcommands (run, status, doctor, exec)
// implemented in [changkun.de/x/wallfacer/internal/cli].
//
// # Connected packages
//
// Depends on [changkun.de/x/wallfacer/internal/cli] for all subcommand logic.
// Changes to CLI subcommand signatures or the embedded filesystem layout require
// updates here.
//
// # Usage
//
//	wallfacer              # Print help
//	wallfacer run          # Start server, restore last workspace group
//	wallfacer doctor       # Check prerequisites and config
//	wallfacer status       # Print board state to terminal
//	wallfacer exec <id>    # Attach to running task container
package main
