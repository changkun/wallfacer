// Package cmdexec provides a fluent command builder and transactional
// command sequencer for subprocess execution.
package cmdexec

import (
	"context"
	"os/exec"
	"strings"
)

// Cmd is a prepared command that can be executed in different output modes.
type Cmd struct {
	name string
	args []string
	ctx  context.Context
}

// New creates a Cmd for the given binary and arguments.
func New(name string, args ...string) *Cmd {
	return &Cmd{name: name, args: args}
}

// Git creates a Cmd for `git -C <dir> <args...>`.
func Git(dir string, args ...string) *Cmd {
	full := make([]string, 0, 2+len(args))
	full = append(full, "-C", dir)
	full = append(full, args...)
	return &Cmd{name: "git", args: full}
}

// WithContext returns a copy of c that uses ctx for cancellation/timeout.
func (c *Cmd) WithContext(ctx context.Context) *Cmd {
	cp := *c
	cp.ctx = ctx
	return &cp
}

// build constructs the underlying os/exec.Cmd, using CommandContext if a
// context was set via WithContext, or plain Command otherwise.
func (c *Cmd) build() *exec.Cmd {
	if c.ctx != nil {
		return exec.CommandContext(c.ctx, c.name, c.args...)
	}
	return exec.Command(c.name, c.args...)
}

// Run executes the command and discards output. Returns nil on success.
func (c *Cmd) Run() error {
	return c.build().Run()
}

// Output executes the command and returns its trimmed stdout.
func (c *Cmd) Output() (string, error) {
	out, err := c.build().Output()
	return strings.TrimSpace(string(out)), err
}

// Combined executes the command and returns trimmed stdout+stderr combined.
func (c *Cmd) Combined() (string, error) {
	out, err := c.build().CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
