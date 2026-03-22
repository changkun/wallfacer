// Package atomicfile provides atomic file write operations using the
// temp-file-then-rename pattern.
package atomicfile

import (
	"encoding/json"
	"os"
)

// Write atomically writes data to path by first writing to a temporary
// file in the same directory and then renaming it to the target path.
// On POSIX systems the rename is atomic, so readers never see a
// partially-written file. The temporary file is cleaned up on failure.
func Write(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp) // best-effort cleanup
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
