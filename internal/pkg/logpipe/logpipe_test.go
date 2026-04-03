package logpipe

import (
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestPipe_BasicOutput verifies that a simple subprocess's stdout lines
// are delivered through the Lines channel.
func TestPipe_BasicOutput(t *testing.T) {
	cmd := exec.Command("echo", "-e", "hello\nworld")
	p, err := Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	var lines []string
	for line := range p.Lines() {
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		t.Fatal("expected output lines")
	}
	got := strings.Join(lines, "\n")
	if !strings.Contains(got, "hello") {
		t.Fatalf("expected 'hello' in output, got %q", got)
	}
}

// TestPipe_MergeStderr verifies that with MergeStderr enabled, both stdout
// and stderr output appear in the Lines channel.
func TestPipe_MergeStderr(t *testing.T) {
	// sh -c prints to both stdout and stderr.
	cmd := exec.Command("sh", "-c", "echo out; echo err >&2")
	p, err := Start(cmd, MergeStderr())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	var lines []string
	for line := range p.Lines() {
		lines = append(lines, line)
	}
	got := strings.Join(lines, "\n")
	if !strings.Contains(got, "out") || !strings.Contains(got, "err") {
		t.Fatalf("expected both stdout and stderr, got %q", got)
	}
}

// TestPipe_StderrDiscardedByDefault verifies that without MergeStderr,
// stderr output is silently discarded and only stdout appears.
func TestPipe_StderrDiscardedByDefault(t *testing.T) {
	cmd := exec.Command("sh", "-c", "echo out; echo err >&2")
	p, err := Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	var lines []string
	for line := range p.Lines() {
		lines = append(lines, line)
	}
	got := strings.Join(lines, "\n")
	if !strings.Contains(got, "out") {
		t.Fatalf("expected stdout 'out', got %q", got)
	}
	if strings.Contains(got, "err") {
		t.Fatalf("stderr should be discarded, got %q", got)
	}
}

// TestPipe_Close verifies that calling Close on a pipe with a long-running
// subprocess causes the Lines channel to drain and close.
func TestPipe_Close(t *testing.T) {
	// A long-running command that we close early.
	cmd := exec.Command("sh", "-c", "while true; do echo tick; sleep 0.01; done")
	p, err := Start(cmd)
	if err != nil {
		t.Fatal(err)
	}

	// Read at least one line.
	select {
	case line, ok := <-p.Lines():
		if !ok {
			t.Fatal("channel closed before any output")
		}
		if line != "tick" {
			t.Fatalf("expected 'tick', got %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for output")
	}

	// Close and verify channel drains.
	p.Close()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-p.Lines():
			if !ok {
				return // success
			}
		case <-deadline:
			t.Fatal("channel not closed after Close()")
		}
	}
}

// TestPipe_Done verifies that the Done channel is closed after the
// subprocess exits and all output has been scanned.
func TestPipe_Done(t *testing.T) {
	cmd := exec.Command("echo", "done")
	p, err := Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	// Drain lines.
	for range p.Lines() { //nolint:revive // intentionally draining
	}

	select {
	case <-p.Done():
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Done channel not closed")
	}
}

// TestPipe_WithBufferSize verifies that custom buffer sizes are applied
// and the pipe still delivers output correctly.
func TestPipe_WithBufferSize(t *testing.T) {
	cmd := exec.Command("echo", "buffered")
	p, err := Start(cmd, WithBufferSize(128, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	var lines []string
	for line := range p.Lines() {
		lines = append(lines, line)
	}
	if len(lines) == 0 || lines[0] != "buffered" {
		t.Fatalf("expected [buffered], got %v", lines)
	}
}

// TestStartReader_CloseDoesNotPanic verifies that calling Close on a pipe
// created via StartReader does not panic. This is a regression test for a bug
// where Close() unconditionally called p.pr.Close() but StartReader sets pr
// to nil, causing a nil pointer dereference.
func TestStartReader_CloseDoesNotPanic(_ *testing.T) {
	r := strings.NewReader("line1\nline2\n")
	p := StartReader(io.NopCloser(r))

	// Drain lines so the scanner goroutine exits.
	for range p.Lines() { //nolint:revive // intentionally empty drain loop
	}
	<-p.Done()

	// This must not panic.
	p.Close()
}

// TestPipe_StartError verifies that Start returns an error when the
// command binary does not exist.
func TestPipe_StartError(t *testing.T) {
	cmd := exec.Command("nonexistent-command-that-does-not-exist")
	_, err := Start(cmd)
	if err == nil {
		t.Fatal("expected error for nonexistent command")
	}
}

// TestStartReader_WithBufferSize verifies that StartReader applies custom
// buffer size options and still delivers output correctly.
func TestStartReader_WithBufferSize(t *testing.T) {
	r := strings.NewReader("alpha\nbeta\n")
	p := StartReader(io.NopCloser(r), WithBufferSize(128, 256))

	var lines []string
	for line := range p.Lines() {
		lines = append(lines, line)
	}
	<-p.Done()

	if len(lines) != 2 || lines[0] != "alpha" || lines[1] != "beta" {
		t.Fatalf("expected [alpha beta], got %v", lines)
	}
}
