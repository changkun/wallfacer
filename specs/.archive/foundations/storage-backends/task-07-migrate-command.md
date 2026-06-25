---
title: "Add wallfacer migrate Command"
status: archived
depends_on:
  - specs/foundations/storage-backends/task-06-composite-backend.md
affects:
  - internal/cli/
effort: medium
created: 2026-03-23
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---


# Task 7: Add `wallfacer migrate` Command

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
   - Copy all blobs via `ReadBlob()` → `SaveBlob()` (outputs, oversight, summaries, tombstones — all use the same blob interface)

3. Add a `migrated` marker file in the filesystem store to prevent double-load.

4. Support `--dry-run` flag to preview what would be migrated without writing.

Note: `internal/store/migrate.go` already exists for schema version upgrades within `task.json`. This is a different concern — cross-backend data migration.

## Acceptance criteria

- `wallfacer migrate` copies all data from filesystem to cloud backend
- Idempotent — running twice does not duplicate data
- `--dry-run` reports counts without writing
- Marker file prevents the server from loading both backends' data
