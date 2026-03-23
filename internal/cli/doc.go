// Package cli implements all wallfacer CLI subcommands: run (start server),
// status (print board state), doctor (check prerequisites), and exec (attach to
// container).
//
// The run subcommand wires together the HTTP server, workspace manager, task store,
// runner, and handler into a running system. Other subcommands provide operational
// tooling for inspecting and interacting with the running server or sandbox
// containers. Platform-specific signal handling and process execution are isolated
// in _unix.go and _windows.go files.
//
// # Connected packages
//
// This is the top-level orchestrator that depends on nearly every internal package:
// [apicontract] for route registration, [handler] for HTTP handlers, [runner] for
// task execution, [store] for persistence, [workspace] for workspace lifecycle,
// [envconfig] for configuration, [logger] for logging, [metrics] for instrumentation,
// [constants] for system parameters, and [prompts] for template management.
// Changes to any of these packages may require corresponding updates in cli.
//
// # Usage
//
//	cli.RunServer(configDir, args, uiFS, docsFS)  // start HTTP server
//	cli.RunStatus(configDir, args)                 // print board state
//	cli.RunDoctor(configDir)                       // check prerequisites
//	cli.RunExec(configDir, args)                   // attach to container
package cli
