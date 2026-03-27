package cmdexec

import (
	"context"
	"fmt"
	"strings"
)

// StepError describes a single command failure with its captured output.
type StepError struct {
	Err    error  // underlying exec error
	Output string // combined stdout+stderr from the command
	Index  int    // step index (0-based)
}

func (e *StepError) Error() string {
	return fmt.Sprintf("step %d failed: %v", e.Index, e.Err)
}

func (e *StepError) Unwrap() error { return e.Err }

// TxError is returned by [Tx.Run] when a step or defer fails. It carries
// the original step failure and any rollback/defer errors, giving the caller
// full control over how to handle each.
type TxError struct {
	Step           *StepError // the forward step that failed (nil if only defers failed)
	RollbackErrors []error    // errors from rollback commands (may be empty)
	DeferErrors    []error    // errors from deferred commands (may be empty)
}

func (e *TxError) Error() string {
	var parts []string
	if e.Step != nil {
		parts = append(parts, e.Step.Error())
	}
	if len(e.RollbackErrors) > 0 {
		parts = append(parts, fmt.Sprintf("%d rollback error(s)", len(e.RollbackErrors)))
	}
	if len(e.DeferErrors) > 0 {
		parts = append(parts, fmt.Sprintf("%d defer error(s)", len(e.DeferErrors)))
	}
	return strings.Join(parts, "; ")
}

func (e *TxError) Unwrap() error {
	if e.Step != nil {
		return e.Step.Err
	}
	return nil
}

// txStep pairs a forward command with an optional rollback command.
type txStep struct {
	cmd      *Cmd
	rollback *Cmd // nil if no rollback is needed for this step
}

// Tx groups commands into a transaction with per-step rollback.
// Steps execute sequentially. On failure, accumulated rollback commands
// run in reverse order, then deferred commands run in LIFO order.
type Tx struct {
	steps  []txStep
	defers []*Cmd
}

// NewTx creates an empty transaction.
func NewTx() *Tx {
	return &Tx{}
}

// Add appends a command with no rollback.
func (tx *Tx) Add(cmd *Cmd) {
	tx.steps = append(tx.steps, txStep{cmd: cmd})
}

// AddWithRollback appends a command with its undo counterpart.
// If this or any later step fails, rollback executes during unwind.
func (tx *Tx) AddWithRollback(cmd, rollback *Cmd) {
	tx.steps = append(tx.steps, txStep{cmd: cmd, rollback: rollback})
}

// Defer registers a command that always runs after the transaction
// completes (success or failure). Deferred commands run in LIFO order.
func (tx *Tx) Defer(cmd *Cmd) {
	tx.defers = append(tx.defers, cmd)
}

// Run executes all steps in order. On first failure, runs accumulated
// rollbacks in reverse, then defers. Returns *TxError with all errors.
// On success, runs defers and returns nil (or *TxError if a defer failed).
func (tx *Tx) Run() error {
	return tx.run(context.Background())
}

// RunContext is like Run but propagates ctx to all commands that don't
// already have their own context.
func (tx *Tx) RunContext(ctx context.Context) error {
	return tx.run(ctx)
}

func (tx *Tx) run(ctx context.Context) error {
	var stepErr *StepError
	failedAt := -1

	for i, step := range tx.steps {
		cmd := step.cmd
		if cmd.ctx == nil {
			cmd = cmd.WithContext(ctx)
		}
		out, err := cmd.Combined()
		if err != nil {
			stepErr = &StepError{Err: err, Output: out, Index: i}
			failedAt = i
			break
		}
	}

	// Rollbacks and defers use context.Background() so they complete even
	// when the caller's context is cancelled (e.g. server shutdown). Cleanup
	// operations like "rebase --abort" or "stash pop" must run regardless.
	cleanupCtx := context.Background()

	var rollbackErrors []error
	if failedAt >= 0 {
		for i := failedAt; i >= 0; i-- {
			rb := tx.steps[i].rollback
			if rb == nil {
				continue
			}
			if rb.ctx == nil {
				rb = rb.WithContext(cleanupCtx)
			}
			if _, err := rb.Combined(); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback step %d: %w", i, err))
			}
		}
	}

	var deferErrors []error
	for i := len(tx.defers) - 1; i >= 0; i-- {
		d := tx.defers[i]
		if d.ctx == nil {
			d = d.WithContext(cleanupCtx)
		}
		if _, err := d.Combined(); err != nil {
			deferErrors = append(deferErrors, fmt.Errorf("defer %d: %w", i, err))
		}
	}

	if stepErr != nil || len(deferErrors) > 0 {
		return &TxError{
			Step:           stepErr,
			RollbackErrors: rollbackErrors,
			DeferErrors:    deferErrors,
		}
	}
	return nil
}
