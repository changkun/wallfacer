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

// Open allocates a new PTY pair, returning the master and slave file
// descriptors. On Linux, the slave must be explicitly unlocked via
// TIOCSPTLCK before it can be opened.
func Open() (master, slave *os.File, err error) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("pty: open /dev/ptmx: %w", err)
	}

	// Unlock the slave side. TIOCSPTLCK with a zero value clears the lock
	// so the slave device (/dev/pts/N) can be opened.
	var unlock int32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, m.Fd(),
		ioctlTIOCSPTLCK, uintptr(unsafe.Pointer(&unlock)))
	if errno != 0 {
		_ = m.Close()
		return nil, nil, fmt.Errorf("pty: TIOCSPTLCK: %w", errno)
	}

	// Get the slave pts number.
	var ptsNum uint32
	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, m.Fd(),
		ioctlTIOCGPTN, uintptr(unsafe.Pointer(&ptsNum)))
	if errno != 0 {
		_ = m.Close()
		return nil, nil, fmt.Errorf("pty: TIOCGPTN: %w", errno)
	}

	slavePath := fmt.Sprintf("/dev/pts/%d", ptsNum)
	s, err := os.OpenFile(slavePath, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		_ = m.Close()
		return nil, nil, fmt.Errorf("pty: open slave %s: %w", slavePath, err)
	}
	return m, s, nil
}
