//go:build windows

// Package pty is not supported on Windows. All functions return errUnsupported.
// Windows PTY support (via ConPTY) may be added in a future phase.
package pty

import (
	"errors"
	"os"
	"os/exec"
)

var errUnsupported = errors.New("pty: not supported on windows")

// Open is a stub that returns errUnsupported on Windows.
func Open() (*os.File, *os.File, error) { return nil, nil, errUnsupported }

// StartWithSize is a stub that returns errUnsupported on Windows.
func StartWithSize(*exec.Cmd, uint16, uint16) (*os.File, error) { return nil, errUnsupported }

// Setsize is a stub that returns errUnsupported on Windows.
func Setsize(*os.File, uint16, uint16) error { return errUnsupported }
