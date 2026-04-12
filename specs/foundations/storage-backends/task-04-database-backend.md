---
title: "Implement DatabaseBackend (PostgreSQL)"
status: archived
depends_on:
  - specs/foundations/storage-backends/task-02-filesystem-backend.md
affects:
  - internal/store/backend_db.go
effort: large
created: 2026-03-23
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---


# Task 4: Implement DatabaseBackend (PostgreSQL)

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

2. Create the schema (tables: `tasks`, `task_events`, `task_blobs`) — see parent spec for DDL.

3. Implement all `StorageBackend` methods:
   - `LoadAll` → `SELECT * FROM tasks WHERE workspace = $1`
   - `SaveTask` → `INSERT ... ON CONFLICT (id) DO UPDATE`
   - `SaveEvent` → `INSERT INTO task_events`
   - `LoadEvents` → `SELECT FROM task_events WHERE task_id = $1 ORDER BY seq`
   - `CompactEvents` → `DELETE` old + `INSERT` compacted in a transaction
   - `SaveBlob` → `INSERT INTO task_blobs ... ON CONFLICT DO UPDATE`
   - `ReadBlob` → `SELECT data FROM task_blobs WHERE task_id = $1 AND key = $2`
   - `DeleteBlob` → `DELETE FROM task_blobs WHERE task_id = $1 AND key = $2`
   - `ListBlobOwners` → `SELECT DISTINCT task_id FROM task_blobs WHERE key = $1`

4. For large blobs (output keys matching `"outputs/*"`), the composite backend (Task 6) routes them to object storage instead. The database backend handles all blob keys, but in practice only small blobs (oversight, summaries, tombstones) will be stored here.

5. Add `WALLFACER_STORAGE_BACKEND=postgres` and `WALLFACER_DATABASE_URL` env vars to `internal/envconfig/`.

6. Add schema migration on startup (embed SQL via `//go:embed`).

## New dependency

- `github.com/lib/pq` or `github.com/jackc/pgx/v5`

## Acceptance criteria

- `DatabaseBackend` implements `StorageBackend` (except output methods)
- Schema auto-created on first connect
- Integration tests against a real PostgreSQL instance (use `testcontainers-go` or skip if no DB available)
- `wallfacer doctor` reports the configured storage backend
