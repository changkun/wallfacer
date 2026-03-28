//go:build windows

// Package pty is not supported on Windows in Phase 1.
package pty

import (
	"errors"
	"os"
	"os/exec"
)

var errUnsupported = errors.New("pty: not supported on windows")

func Open() (*os.File, *os.File, error)                         { return nil, nil, errUnsupported }
func StartWithSize(*exec.Cmd, uint16, uint16) (*os.File, error) { return nil, errUnsupported }
func Setsize(*os.File, uint16, uint16) error                    { return errUnsupported }
