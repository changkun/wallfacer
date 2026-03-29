---
title: "Cloud-Native Search"
status: complete
track: foundations
depends_on:
  - specs/foundations/storage-backends/task-04-database-backend.md
affects:
  - internal/store/
effort: medium
created: 2026-03-23
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 8: Cloud-Native Search

## Goal

Add SQL-based search for the database backend so `SearchTasks` doesn't require loading all tasks into memory.

## Current state

`SearchTasks` (`internal/store/tasks_worktree.go`) does an in-memory scan over `searchIndex map[uuid.UUID]indexedTaskText`, which stores pre-lowercased title, goal, prompt, tags, and oversight text. This works for the filesystem backend where all tasks are already in memory, but won't scale for a database backend serving many workspaces.

## What to do

1. Add a `tsvector` column to the `tasks` table:

```sql
ALTER TABLE tasks ADD COLUMN search_tsv tsvector
    GENERATED ALWAYS AS (
        to_tsvector('english', coalesce(data->>'title','') || ' ' ||
                               coalesce(data->>'goal','') || ' ' ||
                               coalesce(data->>'prompt',''))
    ) STORED;
CREATE INDEX idx_tasks_search ON tasks USING gin(search_tsv);
```

2. Add a `SearchTasks` method to `DatabaseBackend` (or make `Store.SearchTasks` backend-aware):
   - For `FilesystemBackend`: use existing in-memory index
   - For `DatabaseBackend`: use `ts_query` with `ts_rank` ordering

3. Include oversight text in search — either join with `task_oversight` table or denormalize into the `tasks` row.

## Acceptance criteria

- `SearchTasks` returns results from PostgreSQL full-text search when using database backend
- Results ranked by relevance
- Oversight text included in search corpus
- Filesystem backend continues using the in-memory index unchanged
