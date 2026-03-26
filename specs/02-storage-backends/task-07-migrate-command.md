# Task 7: Add `wallfacer migrate` Command

**Status:** Not started
**Depends on:** Task 6
**Effort:** Medium

## Goal

Add a CLI subcommand that migrates task data from the filesystem backend to the cloud backend (PostgreSQL + S3).

## What to do

1. Add `wallfacer migrate` subcommand in `internal/cli/`:
   - Reads from `FilesystemBackend` (source)
   - Writes to `CompositeBackend` (destination)
   - Configured via the same env vars as the server

2. Migration steps:
   - Load all tasks from filesystem via `LoadAll()`
   - Insert each task into the database via `SaveTask()`
   - Copy events via `LoadEvents()` → `SaveEvent()` (preserve sequence numbers)
   - Copy output files via filesystem read → `SaveOutput()`
   - Copy oversight, summaries, tombstones

3. Add a `migrated` marker file in the filesystem store to prevent double-load.

4. Support `--dry-run` flag to preview what would be migrated without writing.

Note: `internal/store/migrate.go` already exists for schema version upgrades within `task.json`. This is a different concern — cross-backend data migration.

## Acceptance criteria

- `wallfacer migrate` copies all data from filesystem to cloud backend
- Idempotent — running twice does not duplicate data
- `--dry-run` reports counts without writing
- Marker file prevents the server from loading both backends' data
