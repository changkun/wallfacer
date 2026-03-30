---
title: "Implement CompositeBackend"
status: complete
depends_on:
  - specs/foundations/storage-backends/task-04-database-backend.md
  - specs/foundations/storage-backends/task-05-object-storage-backend.md
affects:
  - internal/store/backend_composite.go
effort: small
created: 2026-03-23
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 6: Implement CompositeBackend

## Goal

Wire `DatabaseBackend` (tasks + events + small blobs) and `ObjectStorageBackend` (large blobs) together into a single `StorageBackend` that routes each method to the appropriate underlying backend.

## What to do

1. Create `internal/store/backend_composite.go`:

```go
type CompositeBackend struct {
    db       *DatabaseBackend
    blob     *ObjectStorageBackend
    blobKeys map[string]bool // key prefixes routed to blob storage
}

func NewCompositeBackend(db *DatabaseBackend, blob *ObjectStorageBackend) *CompositeBackend
```

2. Route methods:
   - Task CRUD, events → `db`
   - Blob operations → route by key prefix: `"outputs/*"` → `blob`, everything else (`"oversight"`, `"summary"`, `"tombstone"`) → `db`
   - `Init`, `RemoveTask` → both (db record + blob cleanup)
   - `ListBlobOwners` → `db` (small blobs like tombstones are always in DB)

3. Wire into `NewRunner`/`NewStore` selection: when `WALLFACER_STORAGE_BACKEND=postgres`, construct `CompositeBackend` from the configured database and blob backends.

## Acceptance criteria

- `CompositeBackend` implements `StorageBackend`
- Full round-trip test: create task → save output → read back via composite
- `wallfacer doctor` reports `postgres+s3` (or similar) as the active backend
