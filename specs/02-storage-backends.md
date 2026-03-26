# M2: Pluggable Storage Backends

**Status:** Not started | **Date:** 2026-03-23

## Problem

The wallfacer store (`internal/store/`) persists all task data to the local filesystem at `~/.wallfacer/data/<workspace-key>/<task-uuid>/`. State is loaded into memory at startup and kept in sync via atomic file writes. This works for single-machine deployment but breaks cloud deployment in two ways:

1. **Instance lifecycle:** Per-user instances (see `08-cloud-multi-tenant.md`) need to hibernate and wake. If task data is on a local ephemeral disk, it's lost when the instance stops.
2. **Shared access:** If the sandbox executor runs containers remotely (see `01-sandbox-backends.md`), task outputs written inside sandbox pods need to reach the wallfacer server's store.

## Current Architecture

### What the store persists (per task)

| File | Content | Size | Access pattern |
|------|---------|------|----------------|
| `task.json` | Core task record (title, prompt, status, dependencies, sessions, costs) | 1–50 KB | Read on startup, written on every mutation |
| `traces/compact.ndjson` | Compacted event log | 10–500 KB | Append-heavy, read for timeline |
| `traces/NNNN.json` | Individual event files (pre-compaction) | 0.5–5 KB each | Append-only, read for timeline |
| `outputs/turn-NNNN.json` | Raw agent stdout per turn | 10 KB–10 MB | Written by runner, read by UI |
| `outputs/turn-NNNN.stderr.txt` | Raw agent stderr per turn | 1–100 KB | Written by runner, read by UI |
| `oversight.json` | Generated oversight summary | 1–20 KB | Written once, read by UI |
| `tombstone.json` | Soft-delete marker | <1 KB | Written on delete, pruned after retention |
| `summary.json` | Immutable task summary (cost dashboard) | 1–5 KB | Written once at task completion |

### In-memory state derived from disk

- `tasks map[uuid.UUID]*Task` — loaded from `task.json` files at startup
- `events map[uuid.UUID][]TaskEvent` — lazily loaded from `traces/`
- `searchIndex map[uuid.UUID]indexedTaskText` — built from task fields + oversight
- `tasksByStatus map[TaskStatus]map[uuid.UUID]struct{}` — secondary index

### Store interface surface

The `Store` struct (`internal/store/store.go`) exposes ~50 methods. Key categories:

| Category | Methods | I/O pattern |
|----------|---------|-------------|
| Task CRUD | `CreateTaskWithOptions`, `CreateTask`, `mutateTask`, `DeleteTask`, `CancelTask`, `RestoreTask`, `PurgeTask` | Read/write `task.json` via `atomicfile.WriteJSON` |
| Events | `InsertEvent`, `GetEvents`, `GetEventsPage` | Append to `traces/`, read with cursor pagination |
| Outputs | `SaveTurnOutput`, `OutputsDir` | Write/read `outputs/` files directly |
| Search | `SearchTasks`, `RebuildSearchIndex` | In-memory scan of pre-lowercased index |
| Oversight | `SaveOversight`, `GetOversight`, `SaveTestOversight`, `GetTestOversight` | Write/read `oversight.json` |
| Summaries | `SaveSummary`, `LoadSummary` | Write/read `summary.json` |
| Lifecycle | `NewStore(dir string)`, `Close`, `WaitCompaction` | Directory scan, cleanup |
| Pub/Sub | `Subscribe`, `Unsubscribe`, `SubscribeWake` | In-memory change notification |

`NewStore` takes a filesystem directory path only — no backend parameter.

---

## Design: Pluggable Storage Backend

### Interface Extraction

Extract the store's persistence operations into a `StorageBackend` interface. The `Store` struct keeps its in-memory caching, indexing, and pub/sub notification logic but delegates persistence to the backend.

```go
// StorageBackend abstracts where task data is physically stored.
// The Store struct handles in-memory caching, indexing, pub/sub, and
// schema migration; the backend handles only persistence I/O.
type StorageBackend interface {
    // Task persistence
    Init(taskID uuid.UUID) error                   // create task directory/namespace
    LoadAll() ([]*Task, error)                     // startup: load all tasks
    SaveTask(t *Task) error                        // atomic write of task record
    RemoveTask(taskID uuid.UUID) error             // hard delete (after retention)

    // Event persistence
    SaveEvent(taskID uuid.UUID, seq int, event TaskEvent) error
    LoadEvents(taskID uuid.UUID) ([]TaskEvent, int64, error) // events + max seq
    CompactEvents(taskID uuid.UUID, events []TaskEvent) error

    // Output persistence (large blobs)
    SaveOutput(taskID uuid.UUID, turn int, stdout, stderr []byte) error
    ReadOutput(taskID uuid.UUID, filename string) ([]byte, error)

    // Oversight and summaries
    SaveOversight(taskID uuid.UUID, data []byte) error
    ReadOversight(taskID uuid.UUID) ([]byte, error)
    SaveTestOversight(taskID uuid.UUID, data []byte) error
    ReadTestOversight(taskID uuid.UUID) ([]byte, error)
    SaveSummary(taskID uuid.UUID, data []byte) error
    LoadSummary(taskID uuid.UUID) ([]byte, error)

    // Tombstones
    WriteTombstone(taskID uuid.UUID, data []byte) error
    ReadTombstone(taskID uuid.UUID) ([]byte, error)
    DeleteTombstone(taskID uuid.UUID) error
    ListTombstones() ([]uuid.UUID, error)
}
```

### Backend Implementations

#### 1. Filesystem Backend (wraps current behavior)

Extracts the existing file I/O from `Store` methods into a `FilesystemBackend`. Zero behavior change.

```go
type FilesystemBackend struct {
    dir string // e.g., ~/.wallfacer/data/<workspace-key>/
}
```

#### 2. Database Backend (PostgreSQL)

Task metadata and events in PostgreSQL. Suitable for cloud deployment where instances need durable, network-accessible storage.

```sql
-- Core task storage
CREATE TABLE tasks (
    id          UUID PRIMARY KEY,
    workspace   TEXT NOT NULL,
    data        JSONB NOT NULL,     -- serialized Task struct
    status      TEXT NOT NULL,      -- indexed for fast status queries
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL
);
CREATE INDEX idx_tasks_workspace_status ON tasks(workspace, status);

-- Event log (append-only)
CREATE TABLE task_events (
    id          BIGSERIAL PRIMARY KEY,
    task_id     UUID NOT NULL REFERENCES tasks(id),
    seq         INT NOT NULL,
    data        JSONB NOT NULL,
    created_at  TIMESTAMP DEFAULT NOW(),
    UNIQUE(task_id, seq)
);
CREATE INDEX idx_events_task ON task_events(task_id, seq);

-- Oversight summaries
CREATE TABLE task_oversight (
    task_id     UUID PRIMARY KEY REFERENCES tasks(id),
    data        JSONB NOT NULL,
    created_at  TIMESTAMP DEFAULT NOW()
);

-- Task summaries (immutable, for cost dashboard)
CREATE TABLE task_summaries (
    task_id     UUID PRIMARY KEY REFERENCES tasks(id),
    data        JSONB NOT NULL,
    created_at  TIMESTAMP DEFAULT NOW()
);

-- Tombstones (soft-delete markers)
CREATE TABLE task_tombstones (
    task_id     UUID PRIMARY KEY,
    data        JSONB NOT NULL,
    created_at  TIMESTAMP DEFAULT NOW()
);
```

**Large outputs** (turn stdout/stderr) go to object storage (see below), not the database.

#### 3. Object Storage Backend (S3/GCS)

For large blobs (agent outputs, turn files). Used alongside the database backend.

```go
type ObjectStorageBackend struct {
    bucket string
    client *s3.Client // or GCS client
    prefix string     // e.g., "wallfacer/<workspace-key>/"
}
```

**Key layout:**
```
s3://bucket/wallfacer/<workspace-key>/<task-uuid>/outputs/turn-1-stdout
s3://bucket/wallfacer/<workspace-key>/<task-uuid>/outputs/turn-1-stderr
```

#### 4. Composite Backend

Combines database (structured data) and object storage (blobs):

```go
type CompositeBackend struct {
    db   *DatabaseBackend
    blob *ObjectStorageBackend
}

func (c *CompositeBackend) SaveTask(t *Task) error      { return c.db.SaveTask(t) }
func (c *CompositeBackend) SaveOutput(...) error         { return c.blob.SaveOutput(...) }
```

---

## Store Refactoring

### Phase 1: Extract Backend Interface

Separate persistence from in-memory logic. Currently `saveTask` (`internal/store/io.go`) calls `atomicfile.WriteJSON` directly, and `CreateTaskWithOptions` (`internal/store/tasks_create_delete.go`) calls `os.MkdirAll` for directory creation. These filesystem calls move into the backend.

**Before** (current code in `io.go`):
```go
func (s *Store) saveTask(id uuid.UUID, task *Task) error {
    task.SchemaVersion = constants.CurrentTaskSchemaVersion
    pruned := *task
    s.pruneTaskPayload(&pruned)
    path := filepath.Join(s.dir, id.String(), "task.json")
    return atomicfile.WriteJSON(path, &pruned, 0644)
}
```

**After:**
```go
func (s *Store) saveTask(id uuid.UUID, task *Task) error {
    task.SchemaVersion = constants.CurrentTaskSchemaVersion
    pruned := *task
    s.pruneTaskPayload(&pruned)
    return s.backend.SaveTask(&pruned)
}
```

The `Store` struct gains a `backend StorageBackend` field. Construction changes from `NewStore(dir string)` to:
```go
func NewStore(backend StorageBackend) (*Store, error) {
    s := &Store{backend: backend, ...}
    tasks, err := backend.LoadAll()
    // ... populate in-memory maps from loaded tasks
    return s, nil
}
```

### Phase 2: Output Path Handling

The runner currently calls `store.OutputsDir(taskID)` to get a filesystem directory path, and `SaveTurnOutput` writes directly via `os.WriteFile`. The handler serves output files by constructing paths from `OutputsDir`. For non-filesystem backends, this breaks.

**Options:**
1. **Writer interface:** `store.TurnOutputWriter(taskID, filename) → io.WriteCloser` — backend returns a writer (file, S3 upload, etc.)
2. **Byte buffer:** Runner accumulates output in memory, calls `store.SaveTurnOutput(taskID, turn, stdout, stderr)` — already the current API, but the backend would handle storage
3. **Streaming upload:** Runner pipes container stdout through a `TeeReader` that both parses and uploads — most efficient but most complex

**Recommended:** Option 2 (byte buffer) is already the current API shape (`SaveTurnOutput` accepts `[]byte`). The backend just needs to implement where those bytes go. For reading, add `ReadOutput(taskID, filename) ([]byte, error)` to replace direct filesystem path access. Option 1 can be added later if memory pressure from large outputs becomes an issue.

### Phase 3: Search Index

The in-memory search index (`searchIndex`) is rebuilt from loaded tasks at startup. For cloud backends:
- Database backend: `SearchTasks` can use SQL `ILIKE` or full-text search (`tsvector`)
- Filesystem backend: keep current in-memory index
- The `Store` can detect which backend is active and route search accordingly

---

## Migration Path

### Filesystem → Database

For existing users upgrading to cloud deployment:

1. Add a `wallfacer migrate` CLI subcommand
2. Reads all `task.json` files from the filesystem store
3. Inserts into the database in a single transaction
4. Copies output files to object storage
5. Marks the filesystem store as migrated (prevent double-load)

### Backward Compatibility

The filesystem backend remains the default. Cloud storage is opt-in via configuration:

```env
# Default: filesystem (no change for existing users)
WALLFACER_STORAGE_BACKEND=filesystem

# Cloud: PostgreSQL + S3
WALLFACER_STORAGE_BACKEND=postgres
WALLFACER_DATABASE_URL=postgres://user:pass@host:5432/wallfacer
WALLFACER_BLOB_STORAGE=s3
WALLFACER_BLOB_BUCKET=my-wallfacer-bucket
WALLFACER_BLOB_REGION=us-east-1
```

---

## Implementation Tasks

Detailed task breakdowns are in [`02-storage-backends/`](02-storage-backends/).

| # | Task | Depends on | Effort |
|---|------|-----------|--------|
| 1 | [Extract `StorageBackend` interface](02-storage-backends/task-01-extract-interface.md) | — | Medium |
| 2 | [Implement `FilesystemBackend`](02-storage-backends/task-02-filesystem-backend.md) | 1 | Large |
| 3 | [Replace `OutputsDir` with backend methods](02-storage-backends/task-03-replace-outputsdir.md) | 2 | Small |
| 4 | [Implement `DatabaseBackend` (PostgreSQL)](02-storage-backends/task-04-database-backend.md) | 2 | Large |
| 5 | [Implement `ObjectStorageBackend` (S3/GCS)](02-storage-backends/task-05-object-storage-backend.md) | 2 | Medium |
| 6 | [Implement `CompositeBackend`](02-storage-backends/task-06-composite-backend.md) | 4, 5 | Small |
| 7 | [Add `wallfacer migrate` command](02-storage-backends/task-07-migrate-command.md) | 6 | Medium |
| 8 | [Cloud-native search](02-storage-backends/task-08-cloud-search.md) | 4 | Medium |

### Dependencies

- **M1: Sandbox Backends** (`01-sandbox-backends.md`) — complete. The `sandbox.Backend` interface is in place. If future remote backends write outputs inside sandbox pods, the storage backend abstraction handles where those bytes go.
- **Multi-Tenant** (`08-cloud-multi-tenant.md`): Instance provisioning needs to configure the storage backend per user. The database schema includes a `workspace` column for data isolation.
