//go:build linux

package pty

// ioctlErrorCase describes a single ioctl error injection test.
type ioctlErrorCase struct {
	name    string
	failReq uintptr
	wantMsg string
}

// ioctlErrorCases lists the Linux-specific ioctls used by Open.
var ioctlErrorCases = []ioctlErrorCase{
	{"TIOCSPTLCK", ioctlTIOCSPTLCK, "pty: TIOCSPTLCK:"},
	{"TIOCGPTN", ioctlTIOCGPTN, "pty: TIOCGPTN:"},
}
