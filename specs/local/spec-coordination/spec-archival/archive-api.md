---
title: "Archival: Archive/unarchive HTTP endpoints"
status: complete
depends_on:
  - specs/local/spec-coordination/spec-archival/core-model.md
affects:
  - internal/handler/specs.go
  - internal/handler/specs_test.go
effort: small
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Archival: Archive/unarchive HTTP endpoints

## Goal

Add two HTTP endpoints to `internal/handler/specs.go` that let the UI archive and
unarchive a spec. Each endpoint validates the lifecycle transition via `StatusMachine`,
blocks on live dispatched tasks, and atomically rewrites the spec's frontmatter.

## What to do

### New endpoints

Add two handler methods to the `Handler` struct in `internal/handler/specs.go`:

```
POST /api/specs/archive    body: {"path": "specs/local/foo.md"}
POST /api/specs/unarchive  body: {"path": "specs/local/foo.md"}
```

Both share the same shape — only the target status differs:

```go
func (h *Handler) ArchiveSpec(w http.ResponseWriter, r *http.Request) {
    h.transitionSpec(w, r, spec.StatusArchived)
}

func (h *Handler) UnarchiveSpec(w http.ResponseWriter, r *http.Request) {
    h.transitionSpec(w, r, spec.StatusDrafted)
}
```

### `transitionSpec(w, r, toStatus)` implementation

```
1. Decode JSON body → {path string}
2. Find spec file across h.workspaces (reuse findSpecFile helper from specs_dispatch.go)
3. Parse spec frontmatter
4. Validate transition: spec.StatusMachine.Validate(current.Status, toStatus)
   → 422 Unprocessable Entity on invalid transition
5. Guard: if toStatus == StatusArchived && current.DispatchedTaskID != nil
   → 409 Conflict: "cancel the dispatched task before archiving"
   (The store's task status is not checked here; the presence of dispatched_task_id
   is the signal — the user must undispatch first.)
6. spec.UpdateFrontmatter(filePath, map[string]any{
       "status":  string(toStatus),
       "updated": time.Now(),
   })
7. Return 200 OK: {"path": relPath, "status": string(toStatus)}
```

### Router registration

Register the two routes in the router setup (wherever `specs_dispatch` is registered,
likely `internal/cli/server.go` or the router file). Follow the existing pattern for
dispatch:
```go
router.Post("/api/specs/archive",   h.ArchiveSpec)
router.Post("/api/specs/unarchive", h.UnarchiveSpec)
```

## Tests

Add to `specs_test.go`:
- `TestArchiveSpec_Success` — archive a drafted spec: 200, frontmatter updated to `archived`
- `TestArchiveSpec_InvalidTransition` — archive a `vague` spec: 422 with message
  referencing invalid transition
- `TestArchiveSpec_BlockedByDispatch` — archive a spec with `dispatched_task_id` set:
  409 with message containing "cancel"
- `TestArchiveSpec_AlreadyArchived` — archive an already-archived spec: 422 (no same-to-same
  transition in state machine)
- `TestUnarchiveSpec_Success` — unarchive an archived spec: 200, status becomes `drafted`
- `TestUnarchiveSpec_NotArchived` — unarchive a `complete` spec: 422 (invalid transition
  `complete → drafted` is not in the machine — use `stale` first)

## Boundaries

- Do NOT implement multi-spec batch archive in this task (single-path only)
- Do NOT check the task store's live task state; rely only on `dispatched_task_id` presence
- Do NOT touch `specs_dispatch.go` in this task
- Do NOT add UI changes here
- The route path `/api/specs/archive` must not conflict with the existing `/api/specs/*` routes
