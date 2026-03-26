# Task 4: Implement DatabaseBackend (PostgreSQL)

**Status:** Not started
**Depends on:** Task 2
**Effort:** Large

## Goal

Implement a `DatabaseBackend` that stores task metadata, events, oversight, summaries, and tombstones in PostgreSQL. Large outputs (turn stdout/stderr) are excluded — those go to object storage (Task 5).

## What to do

1. Create `internal/store/backend_db.go` with `DatabaseBackend`:

```go
type DatabaseBackend struct {
    db *sql.DB
    workspace string
}

func NewDatabaseBackend(dsn, workspace string) (*DatabaseBackend, error)
```

2. Create the schema (tables: `tasks`, `task_events`, `task_oversight`, `task_summaries`, `task_tombstones`) — see parent spec for DDL.

3. Implement all `StorageBackend` methods:
   - `LoadAll` → `SELECT * FROM tasks WHERE workspace = $1`
   - `SaveTask` → `INSERT ... ON CONFLICT (id) DO UPDATE`
   - `SaveEvent` → `INSERT INTO task_events`
   - `LoadEvents` → `SELECT FROM task_events WHERE task_id = $1 ORDER BY seq`
   - `CompactEvents` → `DELETE` old + `INSERT` compacted in a transaction
   - Oversight/summary/tombstone methods → straightforward CRUD

4. For `SaveOutput`/`ReadOutput`, return `ErrNotSupported` — these are handled by the object storage backend (Task 5) via the composite backend (Task 6).

5. Add `WALLFACER_STORAGE_BACKEND=postgres` and `WALLFACER_DATABASE_URL` env vars to `internal/envconfig/`.

6. Add schema migration on startup (embed SQL via `//go:embed`).

## New dependency

- `github.com/lib/pq` or `github.com/jackc/pgx/v5`

## Acceptance criteria

- `DatabaseBackend` implements `StorageBackend` (except output methods)
- Schema auto-created on first connect
- Integration tests against a real PostgreSQL instance (use `testcontainers-go` or skip if no DB available)
- `wallfacer doctor` reports the configured storage backend
