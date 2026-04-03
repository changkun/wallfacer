//go:build darwin

package pty

// ioctlErrorCase describes a single ioctl error injection test.
type ioctlErrorCase struct {
	name    string
	failReq uintptr
	wantMsg string
}

// ioctlErrorCases lists the macOS-specific ioctls used by Open.
var ioctlErrorCases = []ioctlErrorCase{
	{"TIOCPTYGRANT", ioctlTIOCPTYGRANT, "pty: TIOCPTYGRANT:"},
	{"TIOCPTYUNLK", ioctlTIOCPTYUNLK, "pty: TIOCPTYUNLK:"},
	{"TIOCPTYGNAME", ioctlTIOCPTYGNAME, "pty: TIOCPTYGNAME:"},
}
