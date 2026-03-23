// Package atomicfile provides atomic file write operations using the
// temp-file-then-rename pattern.
//
// Writing directly to a file risks corruption if the process crashes mid-write
// or if readers see partial content. This package writes to a temporary file in
// the same directory, then renames it to the target path, which is atomic on
// POSIX systems. It also provides a JSON convenience wrapper that marshals and
// atomically writes in one step.
//
// # Connected packages
//
// No internal dependencies (stdlib only). Consumed by [envconfig] (atomic .env
// updates), [store] (task metadata and event persistence), [handler] (template
// files), [workspace] (group persistence), and [prompts] (override files).
// This is a foundational utility — changes affect all file persistence paths.
//
// # Usage
//
//	err := atomicfile.Write(path, data, 0o644)
//	err = atomicfile.WriteJSON(path, structValue, 0o644)
package atomicfile
