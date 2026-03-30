---
title: "Implement FilesystemBackend"
status: complete
depends_on:
  - specs/foundations/storage-backends/task-01-extract-interface.md
affects:
  - internal/store/backend_fs.go
  - internal/store/io.go
  - internal/store/store.go
effort: large
created: 2026-03-23
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 2: Implement FilesystemBackend

## Goal

Move all filesystem I/O out of `Store` methods into a `FilesystemBackend` that implements `StorageBackend`. The `Store` delegates all persistence to its backend. Zero behavior change.

## What to do

1. Create `internal/store/backend_fs.go` with `FilesystemBackend`:

```go
type FilesystemBackend struct {
    dir string // e.g., ~/.wallfacer/data/<workspace-key>/
}

func NewFilesystemBackend(dir string) *FilesystemBackend
```

2. Move filesystem operations from these locations into backend methods:
   - `io.go: saveTask` → `atomicfile.WriteJSON` call moves to `backend.SaveTask`
   - `io.go: SaveTurnOutput` → `os.WriteFile` calls move to `backend.SaveBlob` (key = `"outputs/turn-NNNN.json"`, `"outputs/turn-NNNN.stderr.txt"`)
   - `store.go: loadAll` → directory scanning moves to `backend.LoadAll`
   - `events.go: saveEvent` → file write moves to `backend.SaveEvent`
   - `events.go: compactTaskEvents` → compaction write moves to `backend.CompactEvents`
   - `oversight.go` — all `atomicfile.WriteJSON`/`os.ReadFile` calls become `backend.SaveBlob`/`ReadBlob` with keys like `"oversight"`, `"test-oversight"`
   - `tasks_create_delete.go` — `os.MkdirAll` moves to `backend.Init`; tombstone I/O becomes `backend.SaveBlob(id, "tombstone", data)` / `ListBlobOwners("tombstone")`
   - `io.go: SaveSummary/LoadSummary` → `backend.SaveBlob(id, "summary", data)` / `ReadBlob(id, "summary")`

3. Change `NewStore(dir string)` to `NewStore(backend StorageBackend)`. Create a convenience constructor:

```go
func NewFileStore(dir string) (*Store, error) {
    return NewStore(NewFilesystemBackend(dir))
}
```

4. Update all callers of `NewStore` (likely `internal/cli/` and tests).

## Key files

- `internal/store/backend_fs.go` — new file
- `internal/store/io.go` — remove filesystem I/O, delegate to backend
- `internal/store/store.go` — change `NewStore` signature
- `internal/store/events.go` — delegate event persistence
- `internal/store/oversight.go` — delegate oversight persistence
- `internal/store/tasks_create_delete.go` — delegate directory creation and tombstone I/O

## Acceptance criteria

- `FilesystemBackend` implements all `StorageBackend` methods
- `Store` methods contain no direct `os.*`, `filepath.*`, or `atomicfile.*` calls for data persistence
- All existing tests pass
- `NewStore` accepts a `StorageBackend`; existing call sites use `NewFileStore`

## Implementation notes

- **`NewFilesystemBackend` returns `(*FilesystemBackend, error)`** instead of `*FilesystemBackend` — the spec omitted the error return, but the constructor calls `os.MkdirAll` which can fail, so it must propagate the error.
- **`NewFileStore` sets `s.dir` after `NewStore`** rather than passing `dir` as a second parameter to `NewStore`. The `dir` field is kept on Store for backward compatibility with `OutputsDir()` and `DataDir()` until Task 3 removes them.
- **Blob keys use `.json` suffix** (e.g., `"oversight.json"`, `"tombstone.json"`, `"summary.json"`) to match existing filenames, rather than the bare names the spec suggested (e.g., `"oversight"`, `"tombstone"`, `"summary"`). This preserves on-disk compatibility.
- **`currentMaxEventSeq` removed** — replaced by capturing `s.nextSeq[id]-1` under the lock (equivalent since `saveEvent` updates both disk and memory atomically under the lock). This simplifies the compaction path.
- **`compactTaskEvents` now reads events from memory** under a read lock and passes them to `backend.CompactEvents`, rather than the backend re-reading trace files from disk.
- **`FilesystemBackend.LoadAll` persists migrated tasks** directly, rather than the Store layer doing a separate migration-detection pass.
- **`turn_usage.go` left unchanged** — its `ndjson.AppendFile` append semantics don't map cleanly to `SaveBlob` (overwrite). Deferred to a follow-up.
- **`OutputsDir()` and `DataDir()` still use `s.dir`** — deferred to Task 3 as designed.
- **`ListSummaries` uses `ListBlobOwners("summary.json")`** instead of scanning `os.ReadDir(s.dir)` directly.
