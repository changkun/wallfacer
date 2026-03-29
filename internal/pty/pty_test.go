//go:build !windows

package pty

import (
	"bytes"
	"os/exec"
	"testing"
	"time"
)

// TestOpen verifies that Open returns two valid file descriptors
// (master and slave) without error.
func TestOpen(t *testing.T) {
	master, slave, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = master.Close() }()
	defer func() { _ = slave.Close() }()

	if master.Fd() == 0 {
		t.Fatal("master fd is 0")
	}
	if slave.Fd() == 0 {
		t.Fatal("slave fd is 0")
	}
}

// TestStartWithSize verifies that a command started on a PTY produces
// readable output on the master side. The child runs "echo hello" followed
// by "read _" to keep it alive long enough for the master to consume output.
func TestStartWithSize(t *testing.T) {
	// Use "read _" to keep the process alive while we read PTY output.
	// Plain "echo" exits instantly and macOS may return EIO before
	// delivering the buffered output.
	cmd := exec.Command("sh", "-c", "echo hello; read _")
	master, err := StartWithSize(cmd, 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = master.Close() }()

	// Read output from master incrementally; succeed as soon as we see
	// "hello" so we don't depend on EOF timing.
	found := make(chan struct{})
	go func() {
		var out []byte
		buf := make([]byte, 1024)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				out = append(out, buf[:n]...)
				if bytes.Contains(out, []byte("hello")) {
					close(found)
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	select {
	case <-found:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for output")
	}
}

// TestSetsize verifies that Setsize does not return an error when called
// on a valid PTY master with reasonable dimensions.
func TestSetsize(t *testing.T) {
	master, slave, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = master.Close() }()
	defer func() { _ = slave.Close() }()

	if err := Setsize(master, 40, 120); err != nil {
		t.Fatalf("Setsize: %v", err)
	}
}
