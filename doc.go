// Package main is the entry point for the wallfacer CLI application.
//
// Wallfacer is an autonomous engineering platform: a web UI where work moves
// between chat, specs, a task board, and code. Tasks run as host processes in
// per-task git worktrees, and their results are inspected on the board.
//
// This package embeds the frontend UI assets and documentation filesystem into
// the binary via go:embed, then dispatches to CLI subcommands implemented in
// [latere.ai/x/wallfacer/internal/cli].
//
// # Connected packages
//
// Depends on [latere.ai/x/wallfacer/internal/cli] for all subcommand logic.
// Changes to CLI subcommand signatures or the embedded filesystem layout require
// updates here.
//
// # Usage
//
//	wallfacer              # Print help
//	wallfacer run          # Start the task board server
//	wallfacer status       # Print running board state to terminal
//	wallfacer spec         # Spec document tools (new, validate)
//	wallfacer auth         # Sign in to latere.ai (login, logout, whoami)
//	wallfacer web          # Start the cloud web server (wallfacerd)
//	wallfacer doctor       # Check prerequisites and configuration
package main
