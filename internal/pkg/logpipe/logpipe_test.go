package logpipe

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

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

func TestPipe_StartError(t *testing.T) {
	cmd := exec.Command("nonexistent-command-that-does-not-exist")
	_, err := Start(cmd)
	if err == nil {
		t.Fatal("expected error for nonexistent command")
	}
}
