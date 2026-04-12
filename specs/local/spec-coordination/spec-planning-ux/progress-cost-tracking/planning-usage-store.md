---
title: Planning Usage Store Primitive
status: validated
depends_on: []
affects:
  - internal/store/planning_usage.go
  - internal/store/planning_usage_test.go
effort: small
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Planning Usage Store Primitive

## Goal

Add a per-workspace-group append-only log for planning round usage at
`~/.wallfacer/planning/<group-key>/usage.jsonl`, with append and
windowed-read helpers. This is the storage primitive every downstream
task depends on; it must land first.

## What to do

1. Create `internal/store/planning_usage.go`.
2. Add a path-safe group-key helper that matches the existing
   `internal/prompts/instructions.go::InstructionsKey` scheme (first 16
   hex chars of SHA-256 over sorted, normalized workspace paths). Either
   call `InstructionsKey` directly or lift the helper into a shared spot
   and have both call sites use it — keep the two identical, don't fork.
3. Expose:
   - `PlanningUsageDir(root, groupKey string) string` — returns
     `filepath.Join(root, "planning", groupKey)`.
   - `PlanningUsagePath(root, groupKey string) string` — returns
     `filepath.Join(PlanningUsageDir(...), "usage.jsonl")`.
   - `AppendPlanningUsage(root, groupKey string, rec TurnUsageRecord) error`
     — `os.MkdirAll` the dir, then
     `ndjson.AppendFile[TurnUsageRecord](path, rec)`.
   - `ReadPlanningUsage(root, groupKey string, since time.Time) ([]TurnUsageRecord, error)`
     — read the file line by line; skip records where
     `!rec.Timestamp.After(since)` when `since` is non-zero; missing
     file returns `(nil, nil)`.
4. Reuse `internal/pkg/ndjson` for append/read rather than inventing new
   JSON-lines plumbing. Reuse `internal/pkg/atomicfile` only if you need
   atomic rewrites (current design appends, so the ndjson helper suffices).
5. The file writes `TurnUsageRecord` as defined in
   `internal/store/models.go`. Do not define a new struct.

## Tests

Create `internal/store/planning_usage_test.go` with:

- `TestAppendPlanningUsage_RoundtripsRecord` — append a record, read it
  back with `since=time.Time{}`, assert field-by-field equality.
- `TestAppendPlanningUsage_MissingFileReturnsEmpty` — read from a
  never-written group key, expect `(nil, nil)`.
- `TestReadPlanningUsage_FiltersBySince` — append three records with
  increasing timestamps, read with a `since` between the second and
  third; expect only the third.
- `TestPlanningUsageDir_UsesInstructionsKey` — given the same sorted
  path list, assert `PlanningUsageDir` ends in the same 16-char key as
  `InstructionsKey` would produce.
- `TestAppendPlanningUsage_CreatesDir` — call append with a fresh
  group-key dir that doesn't exist; assert the dir and file are created.

## Boundaries

- Do not modify `TurnUsageRecord` or any type in
  `internal/store/models.go`.
- Do not touch `Task`, task-directory layout, tombstones, or search
  index.
- No aggregation logic — only per-group append/read. Aggregation lives
  in the stats/usage handler tasks.
- No handler wiring in this task.
