// Package logpipe manages a subprocess whose stdout is scanned line-by-line
// into a channel. It consolidates the repeated io.Pipe + scanner goroutine +
// cmd.Wait goroutine pattern used by container log streaming handlers.
package logpipe

import (
	"bufio"
	"io"
	"os/exec"
)

// config holds optional settings for a Pipe.
type config struct {
	mergeStderr bool
	bufInitial  int
	bufMax      int
}

// Option configures a Pipe.
type Option func(*config)

// MergeStderr merges the subprocess stderr into stdout so both are
// delivered through the Lines channel. By default stderr is discarded.
func MergeStderr() Option {
	return func(c *config) { c.mergeStderr = true }
}

// WithBufferSize sets the scanner buffer sizes (initial capacity, max).
// Defaults: 64 KB initial, 1 MB max.
func WithBufferSize(initial, maxSize int) Option {
	return func(c *config) {
		c.bufInitial = initial
		c.bufMax = maxSize
	}
}

// Pipe manages a running subprocess whose stdout is scanned line-by-line
// and delivered through a channel.
type Pipe struct {
	lines chan string
	pr    *io.PipeReader
	done  chan struct{}
}

// Start launches cmd, pipes its stdout through a line scanner, and returns
// a Pipe. The Lines channel is closed when the subprocess exits and all
// output has been scanned.
func Start(cmd *exec.Cmd, opts ...Option) (*Pipe, error) {
	cfg := config{
		bufInitial: 64 * 1024,
		bufMax:     1024 * 1024,
	}
	for _, o := range opts {
		o(&cfg)
	}

	pr, pw := io.Pipe()
	cmd.Stdout = pw

	// Handle stderr: merge into stdout or drain separately.
	var stderrPW *io.PipeWriter
	if cfg.mergeStderr {
		cmd.Stderr = pw
	} else {
		var stderrPR *io.PipeReader
		stderrPR, stderrPW = io.Pipe()
		cmd.Stderr = stderrPW

		// Drain stderr so the subprocess is not blocked writing to it.
		go func() {
			_, _ = io.Copy(io.Discard, stderrPR)
			_ = stderrPR.Close()
		}()
	}

	if err := cmd.Start(); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		if stderrPW != nil {
			_ = stderrPW.Close()
		}
		return nil, err
	}

	p := &Pipe{
		lines: make(chan string),
		pr:    pr,
		done:  make(chan struct{}),
	}

	// Close pipe write ends when the subprocess exits.
	go func() {
		_ = cmd.Wait()
		_ = pw.Close()
		if stderrPW != nil {
			_ = stderrPW.Close()
		}
	}()

	// Scan stdout lines into the channel.
	go func() {
		defer close(p.lines)
		defer close(p.done)
		scanner := bufio.NewScanner(pr)
		scanner.Buffer(make([]byte, 0, cfg.bufInitial), cfg.bufMax)
		for scanner.Scan() {
			p.lines <- scanner.Text()
		}
	}()

	return p, nil
}

// StartReader creates a Pipe that scans lines from an existing io.ReadCloser
// instead of launching a subprocess. The Lines channel is closed when the
// reader returns EOF or an error.
func StartReader(r io.ReadCloser, opts ...Option) *Pipe {
	cfg := config{
		bufInitial: 64 * 1024,
		bufMax:     1024 * 1024,
	}
	for _, o := range opts {
		o(&cfg)
	}

	p := &Pipe{
		lines: make(chan string),
		pr:    nil, // no pipe reader to close; caller owns the reader
		done:  make(chan struct{}),
	}

	// Scan lines from the reader in a background goroutine. The lines channel
	// and done channel are both closed when the reader returns EOF or error.
	go func() {
		defer close(p.lines)
		defer close(p.done)
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, cfg.bufInitial), cfg.bufMax)
		for scanner.Scan() {
			p.lines <- scanner.Text()
		}
	}()

	return p
}

// Lines returns the channel delivering one line at a time. The channel
// is closed when the subprocess exits and all output has been scanned.
func (p *Pipe) Lines() <-chan string {
	return p.lines
}

// Done returns a channel that is closed when scanning is complete.
func (p *Pipe) Done() <-chan struct{} {
	return p.done
}

// Close terminates the pipe reader, causing the scanner goroutine to exit.
// Safe to call multiple times. Note: must not be called on pipes created via
// StartReader, which sets pr to nil since the caller owns the reader.
func (p *Pipe) Close() {
	_ = p.pr.Close()
}
