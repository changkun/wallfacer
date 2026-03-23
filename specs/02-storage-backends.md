# Cloud Data Storage

**Date:** 2026-03-23

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
| `outputs/turn-N-{stdout,stderr}` | Raw agent output per turn | 10 KB–10 MB | Written by runner, read by UI |
| `oversight.json` | Generated oversight summary | 1–20 KB | Written once, read by UI |
| `tombstone.json` | Soft-delete marker | <1 KB | Written on delete, pruned after retention |
| `summary.json` | Immutable task summary (cost dashboard) | 1–5 KB | Written once at task completion |

### In-memory state derived from disk

- `tasks map[uuid.UUID]*Task` — loaded from `task.json` files at startup
- `events map[uuid.UUID][]TaskEvent` — lazily loaded from `traces/`
- `searchIndex map[uuid.UUID]indexedTaskText` — built from task fields + oversight
- `tasksByStatus map[TaskStatus]map[uuid.UUID]struct{}` — secondary index

### Store interface surface

The `Store` struct exposes ~40 methods. Key categories:

| Category | Methods | I/O pattern |
|----------|---------|-------------|
| Task CRUD | `CreateTask`, `UpdateTask`, `GetTask`, `ListTasks`, `DeleteTask` | Read/write `task.json` |
| Events | `AppendEvent`, `ListEvents`, `ListEventsCursor` | Append to `traces/`, read with pagination |
| Outputs | `SaveTurnOutput`, `TurnOutputPath` | Write/read `outputs/` files |
| Search | `SearchTasks` | In-memory scan |
| Oversight | `SaveOversight`, `GetOversight` | Write/read `oversight.json` |
| Lifecycle | `NewStore`, `Close`, `WaitCompaction` | Directory scan, cleanup |

---

## Design: Pluggable Storage Backend

### Interface Extraction

Extract the store's persistence operations into a `StorageBackend` interface. The `Store` struct keeps its in-memory caching, indexing, and pub/sub notification logic but delegates persistence to the backend.

```go
// StorageBackend abstracts where task data is physically stored.
type StorageBackend interface {
    // Task persistence
    LoadAll() ([]*Task, error)
    SaveTask(t *Task) error
    DeleteTask(id uuid.UUID) error

    // Event persistence
    AppendEvent(taskID uuid.UUID, event TaskEvent) error
    LoadEvents(taskID uuid.UUID) ([]TaskEvent, error)
    CompactEvents(taskID uuid.UUID, events []TaskEvent) error

    // Output persistence (large blobs)
    SaveOutput(taskID uuid.UUID, filename string, data []byte) error
    ReadOutput(taskID uuid.UUID, filename string) ([]byte, error)
    OutputPath(taskID uuid.UUID, filename string) string // for streaming; may return "" for non-filesystem backends

    // Oversight and summaries
    SaveOversight(taskID uuid.UUID, data []byte) error
    ReadOversight(taskID uuid.UUID) ([]byte, error)
    SaveSummary(taskID uuid.UUID, data []byte) error
    ListSummaries() ([][]byte, error)

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

Separate persistence from in-memory logic in `internal/store/store.go`:

**Before:**
```go
func (s *Store) CreateTask(t *Task) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.tasks[t.ID] = t
    s.addToStatusIndex(t.Status, t.ID)
    s.updateSearchIndex(t)
    // ... write task.json to disk
    s.hub.Publish(TaskDelta{...})
    return nil
}
```

**After:**
```go
func (s *Store) CreateTask(t *Task) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    if err := s.backend.SaveTask(t); err != nil {
        return err
    }
    s.tasks[t.ID] = t
    s.addToStatusIndex(t.Status, t.ID)
    s.updateSearchIndex(t)
    s.hub.Publish(TaskDelta{...})
    return nil
}
```

The `Store` struct gains a `backend StorageBackend` field. Construction:
```go
func NewStore(backend StorageBackend) (*Store, error) {
    s := &Store{backend: backend, ...}
    tasks, err := backend.LoadAll()
    // ... populate in-memory maps from loaded tasks
    return s, nil
}
```

### Phase 2: Output Path Handling

The runner currently calls `store.TurnOutputPath()` to get a filesystem path, then writes directly to it. For non-filesystem backends, this breaks.

**Options:**
1. **Writer interface:** `store.TurnOutputWriter(taskID, filename) → io.WriteCloser` — backend returns a writer (file, S3 upload, etc.)
2. **Byte buffer:** Runner accumulates output in memory, calls `store.SaveTurnOutput(taskID, filename, data)` — simpler but uses more memory for large outputs
3. **Streaming upload:** Runner pipes container stdout through a `TeeReader` that both parses and uploads — most efficient but most complex

**Recommended:** Option 1 (writer interface) for turn outputs since they can be large (10+ MB). Option 2 (byte buffer) for smaller items (oversight, summaries).

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

## Implementation Order

1. **Extract `StorageBackend` interface** from existing `Store` methods — pure refactoring, no behavior change
2. **Implement `FilesystemBackend`** — move file I/O out of `Store` into the backend
3. **Add `TurnOutputWriter` interface** — replace `TurnOutputPath()` with streaming writes
4. **Implement `DatabaseBackend`** — PostgreSQL for task metadata and events
5. **Implement `ObjectStorageBackend`** — S3/GCS for large outputs
6. **Implement `CompositeBackend`** — wire DB + blob together
7. **Add `wallfacer migrate` command** — filesystem → cloud migration tool
8. **Cloud-native search** — SQL full-text search for database backend

### Dependencies on Other Epics

- **Multi-Tenant** (`08-cloud-multi-tenant.md`): Instance provisioning needs to configure the storage backend per user. The database schema includes a `workspace` column for data isolation.
- **Sandbox Executor** (`01-sandbox-backends.md`): If sandbox pods write outputs to a shared volume, the storage backend needs to read from that volume (or the runner needs to relay outputs). The `TurnOutputWriter` interface handles this by abstracting where output bytes go.
