// Package atomicfile provides atomic file write operations using the
// temp-file-then-rename pattern.
package atomicfile

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Write atomically writes data to path by first writing to a temporary
// file in the same directory and then renaming it to the target path.
// On POSIX systems the rename is atomic, so readers never see a
// partially-written file. The temporary file is cleaned up on failure.
func Write(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	// Create the temp file in the same directory as the target so that
	// os.Rename is guaranteed to be an atomic same-filesystem operation.
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()

	// Write and close are separate steps; check both errors independently
	// since Close can flush buffered data and fail on its own.
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		_ = os.Remove(tmp)
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	// Set permissions before rename so the target is never visible with
	// the wrong mode (CreateTemp uses 0600 by default).
	if err := os.Chmod(tmp, perm); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// WriteJSON marshals v as indented JSON and atomically writes the result
// to path. See Write for atomicity guarantees.
func WriteJSON(path string, v any, perm os.FileMode) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return Write(path, raw, perm)
}
