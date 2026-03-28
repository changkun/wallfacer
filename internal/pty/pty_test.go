//go:build !windows

package pty

import (
	"bytes"
	"os/exec"
	"testing"
	"time"
)

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

func TestStartWithSize(t *testing.T) {
	cmd := exec.Command("echo", "hello")
	master, err := StartWithSize(cmd, 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = master.Close() }()

	// Read output from master with a deadline so the test doesn't hang.
	done := make(chan []byte, 1)
	go func() {
		var out []byte
		buf := make([]byte, 1024)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				out = append(out, buf[:n]...)
			}
			if err != nil {
				break
			}
		}
		done <- out
	}()

	select {
	case out := <-done:
		if !bytes.Contains(out, []byte("hello")) {
			t.Fatalf("expected 'hello' in output, got %q", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for output")
	}

	_ = cmd.Wait()
}

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
