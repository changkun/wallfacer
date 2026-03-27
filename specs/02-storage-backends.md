# M2: Pluggable Storage Backends

**Status:** Enablers complete (tasks 1–3); cloud backends deferred | **Date:** 2026-03-23

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
//
// Three concerns: tasks (structured, indexed), events (ordered,
// append-heavy), and blobs (named bytes per task — outputs, oversight,
// summaries, tombstones, etc.).
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

The `Store` layer maps domain concepts to blob keys:

| Store method | Backend call |
|---|---|
| `SaveOversight(id, data)` | `SaveBlob(id, "oversight", data)` |
| `SaveTestOversight(id, data)` | `SaveBlob(id, "test-oversight", data)` |
| `SaveSummary(id, data)` | `SaveBlob(id, "summary", data)` |
| `WriteTombstone(id, data)` | `SaveBlob(id, "tombstone", data)` |
| `ListTombstones()` | `ListBlobOwners("tombstone")` |
| `SaveTurnOutput(id, turn, out, err)` | `SaveBlob(id, "outputs/turn-0001.json", out)` + stderr |

The backend only knows three concepts — tasks, events, blobs — and each implementation maps them to its natural storage:

- **Filesystem**: blob key → file path under task directory
- **PostgreSQL**: blob key → row in a `task_blobs(task_id, key, data)` table
- **S3**: blob key → object key suffix

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

-- Blob storage (oversight, summaries, tombstones, small outputs)
CREATE TABLE task_blobs (
    task_id     UUID NOT NULL REFERENCES tasks(id),
    key         TEXT NOT NULL,       -- e.g., "oversight", "tombstone", "summary"
    data        BYTEA NOT NULL,
    created_at  TIMESTAMP DEFAULT NOW(),
    PRIMARY KEY (task_id, key)
);
CREATE INDEX idx_blobs_key ON task_blobs(key);  -- for ListBlobOwners
```

**Large outputs** (turn stdout/stderr) go to object storage (see below), not the database. Small blobs (oversight, summaries, tombstones) go directly into `task_blobs`.

#### 3. Object Storage Backend (S3/GCS)

For large blobs (agent outputs, turn files). Used alongside the database backend.

```go
type ObjectStorageBackend struct {
    bucket string
    client *s3.Client // or GCS client
    prefix string     // e.g., "wallfacer/<workspace-key>/"
}
```

**Key layout:** blob key maps directly to the S3 object key suffix:
```
s3://bucket/<prefix>/<task-uuid>/<key>
s3://bucket/wallfacer/ws-abc123/deadbeef-1234/outputs/turn-0001.json
s3://bucket/wallfacer/ws-abc123/deadbeef-1234/oversight
```

#### 4. Composite Backend

Combines database (tasks + events + small blobs) and object storage (large blobs):

```go
type CompositeBackend struct {
    db        *DatabaseBackend
    blob      *ObjectStorageBackend
    blobKeys  map[string]bool // keys routed to blob storage (e.g., "outputs/*")
}

func (c *CompositeBackend) SaveTask(t *Task) error { return c.db.SaveTask(t) }
func (c *CompositeBackend) SaveBlob(id uuid.UUID, key string, data []byte) error {
    if c.isBlobKey(key) {
        return c.blob.SaveBlob(id, key, data)
    }
    return c.db.SaveBlob(id, key, data) // small blobs stay in DB
}
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

With the blob interface, outputs become blob keys like `"outputs/turn-0001.json"`. The `Store` maps:
- `SaveTurnOutput(id, turn, stdout, stderr)` → `backend.SaveBlob(id, "outputs/turn-0001.json", stdout)` + `backend.SaveBlob(id, "outputs/turn-0001.stderr.txt", stderr)`
- Output serving in handlers → `backend.ReadBlob(id, key)`

`OutputsDir` is removed; all access goes through the blob interface.

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

### Enablers (complete)

| # | Task | Status | Effort |
|---|------|--------|--------|
| 1 | [Extract `StorageBackend` interface](02-storage-backends/task-01-extract-interface.md) | **Done** | Medium |
| 2 | [Implement `FilesystemBackend`](02-storage-backends/task-02-filesystem-backend.md) | **Done** | Large |
| 3 | [Replace `OutputsDir` with backend methods](02-storage-backends/task-03-replace-outputsdir.md) | **Done** | Small |

### Cloud backends (deferred)

These depend on a concrete cloud deployment target. Implement when needed.

| # | Task | Depends on | Effort |
|---|------|-----------|--------|
| 4 | [Implement `DatabaseBackend` (PostgreSQL)](02-storage-backends/task-04-database-backend.md) | 2 | Large |
| 5 | [Implement `ObjectStorageBackend` (S3/GCS)](02-storage-backends/task-05-object-storage-backend.md) | 2 | Medium |
| 6 | [Implement `CompositeBackend`](02-storage-backends/task-06-composite-backend.md) | 4, 5 | Small |
| 7 | [Add `wallfacer migrate` command](02-storage-backends/task-07-migrate-command.md) | 6 | Medium |
| 8 | [Cloud-native search](02-storage-backends/task-08-cloud-search.md) | 4 | Medium |

### Dependencies

- **M1: Sandbox Backends** (`01-sandbox-backends.md`) — complete. The `sandbox.Backend` interface is in place. If future remote backends write outputs inside sandbox pods, the storage backend abstraction handles where those bytes go.
- **Multi-Tenant** (`08-cloud-multi-tenant.md`): Instance provisioning needs to configure the storage backend per user. The database schema includes a `workspace` column for data isolation.
