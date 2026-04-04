---
title: Spec Frontmatter Writer
status: complete
depends_on: []
affects:
  - internal/spec/
effort: small
created: 2026-04-04
updated: 2026-04-04
author: changkun
dispatched_task_id: null
---

# Spec Frontmatter Writer

## Goal

Add an `UpdateFrontmatter()` function to `internal/spec/` that can write individual fields back to a spec file's YAML frontmatter without disturbing the markdown body. Currently the spec package is read-only (only `ParseFile` and `ParseBytes` exist). The dispatch workflow needs to write `dispatched_task_id`, `status`, and `updated` fields back to spec files atomically.

## What to do

1. In `internal/spec/write.go` (new file), implement `UpdateFrontmatter(path string, updates map[string]any) error`:
   - Read the file content
   - Split into frontmatter (between `---` delimiters) and body (everything after second `---`)
   - Parse the YAML frontmatter into `yaml.Node` (to preserve field ordering, comments, and formatting)
   - For each key in `updates`, find and update the corresponding node value, or append it if missing
   - Re-serialize the YAML frontmatter and concatenate with the original body
   - Write back atomically (temp file + rename)

2. Handle edge cases:
   - `dispatched_task_id: null` vs `dispatched_task_id: "uuid-string"` — null must serialize as YAML `null`, not the string `"null"`
   - `updated` field should accept `time.Time` or `Date` type and serialize in `2006-01-02` format
   - Preserve the exact body content (no trailing newline changes)

3. Validate that `UpdateFrontmatter` round-trips correctly: parse a spec, update a field, parse again, verify the update took effect and no other fields changed.

## Tests

- `TestUpdateFrontmatter_SingleField` — update `status` from `validated` to `complete`, verify body unchanged
- `TestUpdateFrontmatter_MultipleFields` — update `dispatched_task_id` and `updated` together
- `TestUpdateFrontmatter_NullValue` — set `dispatched_task_id` to `nil`, verify YAML serializes as `null`
- `TestUpdateFrontmatter_PreservesBody` — verify markdown body (including code blocks with `---`) is preserved exactly
- `TestUpdateFrontmatter_Roundtrip` — parse, update, parse again, compare all fields
- `TestUpdateFrontmatter_NonexistentFile` — returns error for missing file
- `TestUpdateFrontmatter_InvalidFrontmatter` — returns error for malformed YAML

## Boundaries

- Do NOT add any HTTP handler or API route logic
- Do NOT modify the existing `ParseFile` or `ParseBytes` functions
- Do NOT add full spec serialization (only individual field updates)
- Do NOT handle spec validation (callers are responsible for validating before/after)
