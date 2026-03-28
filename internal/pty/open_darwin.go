//go:build darwin

package pty

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	ioctlTIOCSWINSZ   = 0x80087467 // _IOW('t', 103, struct winsize)
	ioctlTIOCPTYGRANT = 0x20007454 // _IO('t', 84) — grantpt
	ioctlTIOCPTYUNLK  = 0x20007452 // _IO('t', 82) — unlockpt
	ioctlTIOCPTYGNAME = 0x40807453 // _IOC(IOC_OUT, 't', 83, 128) — ptsname
)

// Open allocates a new PTY pair, returning the master and slave file
// descriptors. Uses macOS-specific ioctls (TIOCPTYGRANT, TIOCPTYUNLK,
// TIOCPTYGNAME) to avoid cgo.
func Open() (master, slave *os.File, err error) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("pty: open /dev/ptmx: %w", err)
	}

	fd := m.Fd()

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd,
		ioctlTIOCPTYGRANT, 0); errno != 0 {
		_ = m.Close()
		return nil, nil, fmt.Errorf("pty: TIOCPTYGRANT: %w", errno)
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd,
		ioctlTIOCPTYUNLK, 0); errno != 0 {
		_ = m.Close()
		return nil, nil, fmt.Errorf("pty: TIOCPTYUNLK: %w", errno)
	}

	var name [128]byte
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd,
		ioctlTIOCPTYGNAME, uintptr(unsafe.Pointer(&name[0]))); errno != 0 {
		_ = m.Close()
		return nil, nil, fmt.Errorf("pty: TIOCPTYGNAME: %w", errno)
	}

	end := 0
	for end < len(name) && name[end] != 0 {
		end++
	}
	slavePath := string(name[:end])

	s, err := os.OpenFile(slavePath, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		_ = m.Close()
		return nil, nil, fmt.Errorf("pty: open slave %s: %w", slavePath, err)
	}
	return m, s, nil
}
