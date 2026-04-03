//go:build !windows

package pty

import (
	"bytes"
	"errors"
	"os"
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

// TestSetsizeInvalidFd verifies that Setsize returns an error when called
// on a non-PTY file descriptor (e.g. a regular file).
func TestSetsizeInvalidFd(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "not-a-pty")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if err := Setsize(f, 24, 80); err == nil {
		t.Fatal("expected error for non-PTY fd, got nil")
	}
}

// TestStartWithSizeBadCommand verifies that StartWithSize returns an error
// when the command cannot be started.
func TestStartWithSizeBadCommand(t *testing.T) {
	cmd := exec.Command("/nonexistent-binary-that-does-not-exist")
	master, err := StartWithSize(cmd, 24, 80)
	if err == nil {
		_ = master.Close()
		t.Fatal("expected error for nonexistent command, got nil")
	}
}

// TestOpenPtmxError verifies that Open returns a wrapped error when the
// PTY multiplexer device cannot be opened.
func TestOpenPtmxError(t *testing.T) {
	origOpen := openPtmx
	t.Cleanup(func() { openPtmx = origOpen })

	openPtmx = func() (*os.File, error) {
		return nil, errors.New("synthetic ptmx error")
	}

	_, _, err := Open()
	if err == nil {
		t.Fatal("expected error when openPtmx fails")
	}
	if got := err.Error(); got != "pty: open /dev/ptmx: synthetic ptmx error" {
		t.Fatalf("unexpected error: %s", got)
	}
}

// TestOpenIoctlErrors verifies that Open returns wrapped errors when each
// ioctl call fails. The test cases are platform-specific (see
// open_darwin_test.go / open_linux_test.go).
func TestOpenIoctlErrors(t *testing.T) {
	for _, tt := range ioctlErrorCases {
		t.Run(tt.name, func(t *testing.T) {
			origIoctl := ioctl
			t.Cleanup(func() { ioctl = origIoctl })

			ioctl = func(fd, req, arg uintptr) error {
				if req == tt.failReq {
					return errors.New("injected ioctl error")
				}
				return origIoctl(fd, req, arg)
			}

			_, _, err := Open()
			if err == nil {
				t.Fatalf("expected error for %s failure", tt.name)
			}
			if got := err.Error(); len(got) < len(tt.wantMsg) || got[:len(tt.wantMsg)] != tt.wantMsg {
				t.Fatalf("expected error starting with %q, got %q", tt.wantMsg, got)
			}
		})
	}
}

// TestOpenSlaveError verifies that Open returns a wrapped error when the
// slave device cannot be opened.
func TestOpenSlaveError(t *testing.T) {
	origSlave := openSlave
	t.Cleanup(func() { openSlave = origSlave })

	openSlave = func(_ string) (*os.File, error) {
		return nil, errors.New("synthetic slave error")
	}

	_, _, err := Open()
	if err == nil {
		t.Fatal("expected error when openSlave fails")
	}
	if got := err.Error(); !bytes.Contains([]byte(got), []byte("open slave")) {
		t.Fatalf("expected 'open slave' in error, got %q", got)
	}
}

// TestStartWithSizeOpenError verifies that StartWithSize propagates errors
// from the PTY allocator.
func TestStartWithSizeOpenError(t *testing.T) {
	orig := openFunc
	t.Cleanup(func() { openFunc = orig })

	openFunc = func() (*os.File, *os.File, error) {
		return nil, nil, errors.New("synthetic open error")
	}

	cmd := exec.Command("echo", "hello")
	master, err := StartWithSize(cmd, 24, 80)
	if err == nil {
		_ = master.Close()
		t.Fatal("expected error when Open fails, got nil")
	}
	if !errors.Is(err, err) || err.Error() != "synthetic open error" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestStartWithSizeSetsizeError verifies that StartWithSize returns an error
// and closes the master when Setsize fails (e.g. invalid PTY fd).
func TestStartWithSizeSetsizeError(t *testing.T) {
	orig := openFunc
	t.Cleanup(func() { openFunc = orig })

	// Return a regular file as "master" so Setsize fails with ENOTTY,
	// and a real slave so the defer close doesn't panic.
	openFunc = func() (*os.File, *os.File, error) {
		fakeMaster, err := os.CreateTemp(t.TempDir(), "fake-master")
		if err != nil {
			t.Fatal(err)
		}
		fakeSlave, err := os.CreateTemp(t.TempDir(), "fake-slave")
		if err != nil {
			_ = fakeMaster.Close()
			t.Fatal(err)
		}
		return fakeMaster, fakeSlave, nil
	}

	cmd := exec.Command("echo", "hello")
	master, err := StartWithSize(cmd, 24, 80)
	if err == nil {
		_ = master.Close()
		t.Fatal("expected error when Setsize fails, got nil")
	}
}
