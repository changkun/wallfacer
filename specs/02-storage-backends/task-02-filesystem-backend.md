# Task 2: Implement FilesystemBackend

**Status:** Not started
**Depends on:** Task 1
**Effort:** Large

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
   - `io.go: SaveTurnOutput` → `os.WriteFile` calls move to `backend.SaveOutput`
   - `store.go: loadAll` → directory scanning moves to `backend.LoadAll`
   - `events.go: saveEvent` → file write moves to `backend.SaveEvent`
   - `events.go: compactTaskEvents` → compaction write moves to `backend.CompactEvents`
   - `oversight.go` — all `atomicfile.WriteJSON`/`os.ReadFile` calls move to backend
   - `tasks_create_delete.go` — `os.MkdirAll` moves to `backend.Init`, tombstone I/O to backend

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
