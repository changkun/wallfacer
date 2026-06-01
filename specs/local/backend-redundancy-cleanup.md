---
title: Backend Redundancy Cleanup
status: drafted
depends_on: []
affects:
  - internal/
  - specs/local/backend-redundancy-cleanup/
effort: large
created: 2026-06-01
updated: 2026-06-01
author: changkun
dispatched_task_id: null
---

# Backend Redundancy Cleanup

Umbrella for follow-ups to the June 2026 backend cleanup. Pass 1
landed 14 small commits on `main` (see **Pass 1 — done** below). The
remaining items split cleanly into two groups:

- **Backend-only** children carry no `depends_on` for the frontend
  and can be dispatched immediately.
- **API-surface** children depend on
  [vue-frontend-migration.md](vue-frontend-migration.md) because each
  one touches both `ui/` and `frontend/` plus the generated
  `ui/js/generated/routes.js`.

Each child below is a leaf spec sized for one task. Dispatch them
individually rather than treating this umbrella as a single unit.

## Backend-only (run anytime)

| Spec | Status | What |
|---|---|---|
| [taskusage-cache-fix.md](backend-redundancy-cleanup/taskusage-cache-fix.md) | **Done** | Fixed the `CacheReadInputTokens` / `CacheCreationTokens` undercount in `/api/stats` bucket aggregations. Extracted `(*TaskUsage).Add` so the handler, store, and stats layers stop inlining five-field `+=` loops with different field subsets. |
| [handler-helpers-dedup.md](backend-redundancy-cleanup/handler-helpers-dedup.md) | **Done** | Four micro-cleanups bundled: unified the two `bearerToken` helpers; replaced `hasScope`/`hasAud` with `slices.Contains`; promoted `parsePathID` to `httpjson.PathUUID` and migrated inline UUID parses; routed `orgs.SwitchOrg` through `httpjson.DecodeBody`. |
| [background-task-launcher.md](backend-redundancy-cleanup/background-task-launcher.md) | **Done** | Collapsed `SyncWorktreesBackground`, `GenerateOversightBackground`, and `GenerateTitleBackground` into a single `r.taskBackground(label, taskID, fn)` helper. `RunBackground` stays separate (has workspace-counting logic). |
| [cache-wrappers-inline.md](backend-redundancy-cleanup/cache-wrappers-inline.md) | Archived | Decided against. The diffcache `set` immutable-branch and commitsbehind `cachedCommitsBehind` read-through are real policy; inlining would push them to every caller for negative LOC and worse readability. Eleven tests would need rewriting. See Outcome in the child spec. |
| [agents-flow-generic-registry.md](backend-redundancy-cleanup/agents-flow-generic-registry.md) | Archived | Decided against. The asymmetry (flow clones on Get; agents doesn't) and the three `Resolve*` methods only on flow.Registry mean a shared `Registry[T]` needs a clone hook + wrapper types + Resolve* forwarding, totalling more LOC than the duplication it removed. The smaller `ResolveForTask` / `ResolveRoutineFlow` consolidation landed as a focused micro-refactor in `flow/registry.go`. |

## API-surface (block on vue-frontend-migration)

Each of these touches the API contract, the generated frontend
routes table, and both `ui/` (legacy) and `frontend/` (Vue). Each
collapses 2–4 verb-specific routes into one PATCH (or one
parameterised endpoint).

| Spec | Effort | Removes |
|---|---|---|
| [api-planning-threads-patch.md](backend-redundancy-cleanup/api-planning-threads-patch.md) | Small | 3 POSTs (archive/unarchive/activate) → 1 PATCH. Handler bodies already share `mutatePlanningThread`. |
| [api-spec-actions-collapse.md](backend-redundancy-cleanup/api-spec-actions-collapse.md) | Medium | 4 POSTs (dispatch/undispatch/archive/unarchive) → 1 `/api/specs/transition` with `action`. Internal logic stays per-action; only the HTTP edge moves. |
| [api-oversight-phase.md](backend-redundancy-cleanup/api-oversight-phase.md) | Small | `/oversight/test` sibling → `?phase=test` query on the base route. |
| [api-task-actions-patch.md](backend-redundancy-cleanup/api-task-actions-patch.md) | Medium | 4 POSTs (cancel/archive/unarchive/restore) → PATCH body fields. Keeps `done`/`resume`/`sync`/`test` as dedicated side-effect endpoints. Also lands the cross-cutting `transitionTask` helper. |
| [api-auth-org-patch.md](backend-redundancy-cleanup/api-auth-org-patch.md) | Small | `POST /api/auth/switch-org` → `PATCH /api/auth/me`. |
| [api-ideate-routines.md](backend-redundancy-cleanup/api-ideate-routines.md) | Small | Either remove the `/api/ideate` triple (route ideation through the routines list filter) or document the facade. |

## Considered but skipped

These came up in the analysis passes and are intentionally **not**
in scope:

- **`httpjson.Error` mass migration.** 361 sites mix `http.Error`
  (plain text) and `httpjson.Write(..., map["error":...])` (JSON).
  Adding a helper is one line; migrating all 361 is a large
  cross-cutting churn for modest payoff. Defer until the frontend
  needs structured errors uniformly.
- **`Auth` vs `OptionalAuth` middleware dedup.** Differ only in the
  fail branch; parameterising hurts readability more than the
  deduplication helps.
- **`StreamLogs` / `StreamPlanningMessages` adoption of
  `internal/pkg/sse`.** Both emit `text/plain` with raw `\n`
  keepalives, not SSE frames. Wrong protocol.
- **Per-field `UpdateTaskX` setter consolidation.** ~25 typed setters
  in `store/tasks_update.go`; a generic `UpdateTaskFields(partial)`
  would trade type safety for line count.
- **`internal/runner` 27k-LOC audit.** Deserves its own pass, not
  this umbrella.

## Pass 1 — done

For context — these landed in early June 2026 and should not appear
in any follow-up plan:

- `internal/pkg/sse` — SSE Writer. Adopted by every real SSE handler
  (`SpecTreeStream`, `ExplorerStream`, `StreamImagePull`,
  `StreamTasks`, `GitStatusStream`).
- `internal/pkg/yamlwatch` — debounced fsnotify watcher. Replaced
  byte-identical copies in `agents/watch.go` and `flow/watch.go`
  (-335 LOC).
- `internal/pkg/slugutil` — kebab-case validator. `agents.IsValidSlug`
  and `flow.IsValidSlug` wrappers later removed; callers use
  `slugutil.IsValid` direct.
- `internal/pkg/yamldir` — YAML directory loader scaffold. Used by
  `agents.LoadUserAgents` and `flow.LoadUserFlows`.
- `atomicfile.Write` adopted in `agents.WriteUserAgent` and
  `flow.WriteUserFlow`.
- `httpjson.Write` / `DecodeOptionalBody` adopted in `device_auth.go`
  (dropped local `writeJSON`).
- `runBackfillBatch` helper unifies `GenerateMissingTitles` +
  `GenerateMissingOversight`.
- `mutatePlanningThread` collapses the planning-thread handler
  bodies (the external POSTs collapse in the API-surface child
  spec).
- `GetTurnUsage` routed through the `withID` shim.
