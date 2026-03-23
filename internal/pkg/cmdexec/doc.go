// Package cmdexec provides a fluent command builder and transactional command
// sequencer with rollback and deferred cleanup.
//
// [Cmd] wraps os/exec.Cmd with a builder pattern for common operations: setting
// the working directory, capturing stdout/stderr, and running with context. The
// [Git] constructor provides a shorthand for git -C commands. [Tx] sequences
// multiple commands with optional per-step rollback and LIFO deferred cleanup,
// ensuring that partial operations are unwound on failure.
//
// # Connected packages
//
// No internal dependencies (stdlib only). Consumed by [gitutil] (all git CLI
// operations) and [cli] (doctor checks, exec subcommand, container management).
// This is the lowest-level command execution layer — changes to error handling
// or output capture affect all git and container operations.
//
// # Usage
//
//	out, err := cmdexec.Git(repoDir, "status", "--porcelain").Output()
//	tx := cmdexec.NewTx()
//	tx.Add(cmdexec.Git(dir, "checkout", "-b", branch))
//	tx.AddWithRollback(cmd, rollbackCmd)
//	tx.Defer(cleanupCmd)
//	err = tx.RunContext(ctx)
package cmdexec
