# Task 1: Extract StorageBackend Interface

**Status:** Not started
**Depends on:** None
**Effort:** Medium

## Goal

Define a `StorageBackend` interface that captures all persistence I/O currently hardcoded in the `Store` struct. No behavior change — this is a pure refactoring step that prepares for pluggable backends.

## What to do

1. Create `internal/store/backend.go` with the `StorageBackend` interface:

```go
type StorageBackend interface {
    Init(taskID uuid.UUID) error
    LoadAll() ([]*Task, error)
    SaveTask(t *Task) error
    RemoveTask(taskID uuid.UUID) error

    SaveEvent(taskID uuid.UUID, seq int, event TaskEvent) error
    LoadEvents(taskID uuid.UUID) ([]TaskEvent, int64, error)
    CompactEvents(taskID uuid.UUID, events []TaskEvent) error

    SaveOutput(taskID uuid.UUID, turn int, stdout, stderr []byte) error
    ReadOutput(taskID uuid.UUID, filename string) ([]byte, error)

    SaveOversight(taskID uuid.UUID, data []byte) error
    ReadOversight(taskID uuid.UUID) ([]byte, error)
    SaveTestOversight(taskID uuid.UUID, data []byte) error
    ReadTestOversight(taskID uuid.UUID) ([]byte, error)
    SaveSummary(taskID uuid.UUID, data []byte) error
    LoadSummary(taskID uuid.UUID) ([]byte, error)

    WriteTombstone(taskID uuid.UUID, data []byte) error
    ReadTombstone(taskID uuid.UUID) ([]byte, error)
    DeleteTombstone(taskID uuid.UUID) error
    ListTombstones() ([]uuid.UUID, error)
}
```

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
