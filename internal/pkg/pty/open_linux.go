//go:build linux

package pty

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	ioctlTIOCSWINSZ = 0x5414     // TIOCSWINSZ
	ioctlTIOCSPTLCK = 0x40045431 // _IOW('T', 0x31, int)
	ioctlTIOCGPTN   = 0x80045430 // _IOR('T', 0x30, unsigned int)
)

// openPtmx opens the PTY multiplexer device. Replaceable for testing.
var openPtmx = func() (*os.File, error) {
	return os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
}

// ioctl wraps the SYS_IOCTL syscall. Replaceable for testing.
var ioctl = func(fd, req, arg uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg)
	if errno != 0 {
		return errno
	}
	return nil
}

// openSlave opens the slave PTY device by path. Replaceable for testing.
var openSlave = func(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDWR|syscall.O_NOCTTY, 0)
}

// Open allocates a new PTY pair, returning the master and slave file
// descriptors. On Linux, the slave must be explicitly unlocked via
// TIOCSPTLCK before it can be opened.
func Open() (master, slave *os.File, err error) {
	m, err := openPtmx()
	if err != nil {
		return nil, nil, fmt.Errorf("pty: open /dev/ptmx: %w", err)
	}

	// Unlock the slave side. TIOCSPTLCK with a zero value clears the lock
	// so the slave device (/dev/pts/N) can be opened.
	var unlock int32
	if err := ioctl(m.Fd(), ioctlTIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); err != nil {
		_ = m.Close()
		return nil, nil, fmt.Errorf("pty: TIOCSPTLCK: %w", err)
	}

	// Get the slave pts number.
	var ptsNum uint32
	if err := ioctl(m.Fd(), ioctlTIOCGPTN, uintptr(unsafe.Pointer(&ptsNum))); err != nil {
		_ = m.Close()
		return nil, nil, fmt.Errorf("pty: TIOCGPTN: %w", err)
	}

	slavePath := fmt.Sprintf("/dev/pts/%d", ptsNum)
	s, err := openSlave(slavePath)
	if err != nil {
		_ = m.Close()
		return nil, nil, fmt.Errorf("pty: open slave %s: %w", slavePath, err)
	}
	return m, s, nil
}
