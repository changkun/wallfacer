//go:build !windows

// Package pty provides minimal PTY allocation for macOS and Linux.
// It wraps POSIX syscalls directly to avoid external dependencies.
package pty

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

// winsize mirrors the kernel's struct winsize used by TIOCSWINSZ.
// X and Y are pixel dimensions; they are unused here but must be
// present to match the kernel struct layout.
type winsize struct {
	Row uint16
	Col uint16
	X   uint16
	Y   uint16
}

// Setsize sets the terminal window size on the PTY master.
func Setsize(f *os.File, rows, cols uint16) error {
	ws := winsize{Row: rows, Col: cols}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(),
		ioctlTIOCSWINSZ, uintptr(unsafe.Pointer(&ws)))
	if errno != 0 {
		return errno
	}
	return nil
}

// StartWithSize spawns cmd attached to a new PTY with the given window size.
// The returned file is the PTY master; the caller must close it when done.
func StartWithSize(cmd *exec.Cmd, rows, cols uint16) (*os.File, error) {
	master, slave, err := Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = slave.Close() }()

	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	// Start a new session and make the slave the controlling terminal.
	// Ctty=0 refers to the child's fd 0 (stdin), which is the slave PTY.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
		Ctty:    0,
	}

	if err := Setsize(master, rows, cols); err != nil {
		_ = master.Close()
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		_ = master.Close()
		return nil, err
	}
	return master, nil
}
