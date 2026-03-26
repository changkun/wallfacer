# Task 1: Extract StorageBackend Interface

**Status:** Not started
**Depends on:** None
**Effort:** Medium

## Goal

Define a `StorageBackend` interface that captures all persistence I/O currently hardcoded in the `Store` struct. No behavior change — this is a pure refactoring step that prepares for pluggable backends.

## What to do

1. Create `internal/store/backend.go` with the `StorageBackend` interface:

```go
// Three concerns: tasks (structured, indexed), events (ordered,
// append-heavy), and blobs (named bytes per task).
type StorageBackend interface {
    // Tasks (structured, indexed)
    Init(taskID uuid.UUID) error
    LoadAll() ([]*Task, error)
    SaveTask(t *Task) error
    RemoveTask(taskID uuid.UUID) error

    // Events (ordered, append-heavy)
    SaveEvent(taskID uuid.UUID, seq int, event TaskEvent) error
    LoadEvents(taskID uuid.UUID) ([]TaskEvent, int64, error)
    CompactEvents(taskID uuid.UUID, events []TaskEvent) error

    // Blobs (named bytes per task)
    SaveBlob(taskID uuid.UUID, key string, data []byte) error
    ReadBlob(taskID uuid.UUID, key string) ([]byte, error)
    DeleteBlob(taskID uuid.UUID, key string) error
    ListBlobOwners(key string) ([]uuid.UUID, error)
}
```

The Store layer maps domain concepts to blob keys (e.g., `SaveOversight` → `SaveBlob(id, "oversight", data)`, `SaveTurnOutput` → `SaveBlob(id, "outputs/turn-0001.json", data)`, `ListTombstones` → `ListBlobOwners("tombstone")`).

2. Add a `backend StorageBackend` field to the `Store` struct.

3. Do **not** move any existing code yet — that happens in Task 2.

## Key files

- `internal/store/store.go` — `Store` struct definition, `NewStore`
- `internal/store/io.go` — `saveTask`, `SaveTurnOutput`
- `internal/store/events.go` — `saveEvent`, `compactTaskEvents`
- `internal/store/oversight.go` — oversight and test oversight I/O
- `internal/store/tasks_create_delete.go` — directory creation, tombstone I/O

## Acceptance criteria

- Interface defined and compiles
- `Store` struct has a `backend` field
- All existing tests pass unchanged
