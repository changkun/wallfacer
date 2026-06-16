// Package atomicfile provides atomic file write operations using the
// temp-file-then-rename pattern.
package atomicfile

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Hooks for testing; production code leaves these at their defaults.
var (
	writeFile  = func(f *os.File, data []byte) (int, error) { return f.Write(data) }
	syncFile   = func(f *os.File) error { return f.Sync() }
	closeFile  = func(f *os.File) error { return f.Close() }
	chmodPath  = os.Chmod
	renamePath = os.Rename
)

// Write atomically writes data to path by first writing to a temporary
// file in the same directory and then renaming it to the target path.
// On POSIX systems the rename is atomic, so readers never see a
// partially-written file. The temporary file is cleaned up on failure.
//
// Write does NOT fsync the temp file before rename: the rename is atomic so
// readers never observe a partial file, but a crash immediately after the
// rename can leave the new (renamed) file empty until the OS flushes. This is
// the right trade-off for the append-heavy callers (event traces, task.json)
// where per-write fsync cost outweighs the durability gain. Use [WriteSync]
// for files where crash durability matters more than write throughput.
func Write(path string, data []byte, perm os.FileMode) error {
	return write(path, data, perm, false)
}

// WriteSync behaves like [Write] but fsyncs the temp file before renaming, so
// a crash right after the (atomic) rename cannot leave a renamed-but-empty
// file. Use it for low-frequency writes where durability matters (e.g. the
// specs README); prefer [Write] on hot, append-heavy paths.
func WriteSync(path string, data []byte, perm os.FileMode) error {
	return write(path, data, perm, true)
}

func write(path string, data []byte, perm os.FileMode, sync bool) error {
	dir := filepath.Dir(path)
	// Create the temp file in the same directory as the target so that
	// os.Rename is guaranteed to be an atomic same-filesystem operation.
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()

	// Write, (optional) sync, and close are separate steps; check each error
	// independently since Close can flush buffered data and fail on its own.
	_, writeErr := writeFile(f, data)
	var syncErr error
	if writeErr == nil && sync {
		syncErr = syncFile(f)
	}
	closeErr := closeFile(f)
	if writeErr != nil {
		_ = os.Remove(tmp)
		return writeErr
	}
	if syncErr != nil {
		_ = os.Remove(tmp)
		return syncErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	// Set permissions before rename so the target is never visible with
	// the wrong mode (CreateTemp uses 0600 by default).
	if err := chmodPath(tmp, perm); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := renamePath(tmp, path); err != nil {
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
