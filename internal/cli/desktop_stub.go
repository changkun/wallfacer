//go:build !desktop

package cli

import (
	"errors"
	"io/fs"
)

// RunDesktop is a stub for builds without the desktop tag.
// It returns an error indicating that the desktop mode is unsupported.
func RunDesktop(_ string, _ []string, _, _ fs.FS) error {
	return errors.New("desktop mode unsupported: rebuild with -tags desktop")
}
