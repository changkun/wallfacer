---
title: Backend Redundancy Cleanup
status: drafted
depends_on:
  - specs/local/vue-frontend-migration.md
affects:
  - internal/apicontract/routes.go
  - internal/handler/
  - internal/store/
  - internal/agents/
  - internal/flow/
  - internal/pkg/
  - internal/cli/server.go
  - frontend/src/
  - ui/js/
effort: large
created: 2026-06-01
updated: 2026-06-01
author: changkun
dispatched_task_id: null
---

# Backend Redundancy Cleanup

Post-frontend-migration cleanup pass. A first round of backend
redundancy elimination landed in 14 small commits on `main` in early
June 2026 (see the **Completed in pass 1** section below). The
remaining items either need coordinated frontend changes (so they wait
on [vue-frontend-migration.md](vue-frontend-migration.md) finishing)
or were surfaced by a second analysis pass after pass 1 shipped.

This spec captures everything still open so we can pick it up without
re-doing the analysis.

## Why this is deferred

The biggest remaining wins are **API-shape collapses**: turn three
verb-specific POST endpoints into one PATCH, fold a `/oversight/test`
sibling into a `?phase=` query, etc. Each of those requires touching
both `ui/` (legacy vanilla JS) and `frontend/` (Vue rewrite) plus the
generated `ui/js/generated/routes.js`. Doing them during the migration
would force every change to be made in both fronts; doing them after
the migration means each change lands in one place.

The smaller backend-only items in pass 2 don't strictly need the
migration to finish, but bundling them with the API-shape work keeps
the cleanup PRs cohesive (one "API surface reduction" sweep, one
"shared structure" sweep).

## Completed in pass 1 (do not re-do)

For context — these already shipped and should not appear in any
follow-up plan:

- `internal/pkg/sse` — SSE Writer. Adopted by `SpecTreeStream`,
  `ExplorerStream`, `StreamImagePull`, `StreamTasks`, `GitStatusStream`
  (every endpoint that emits real SSE). `StreamLogs` and
  `StreamPlanningMessages` are `text/plain` chunked streams, not SSE,
  and intentionally left alone.
- `internal/pkg/yamlwatch` — debounced fsnotify directory watcher.
  Replaces byte-identical copies in `internal/agents/watch.go` and
  `internal/flow/watch.go` (335 LOC removed).
- `internal/pkg/slugutil` — kebab-case validator. `agents.IsValidSlug`
  and `flow.IsValidSlug` are now thin wrappers.
- `internal/pkg/yamldir` — YAML directory loader scaffold. Consumed by
  `agents.LoadUserAgents` and `flow.LoadUserFlows`.
- `internal/pkg/atomicfile.Write` adoption in `agents.WriteUserAgent`
  and `flow.WriteUserFlow` (replaces hand-rolled temp + rename).
- `internal/pkg/httpjson.Write` / `DecodeOptionalBody` adoption in
  `device_auth.go` (dropped the local `writeJSON`).
- `runBackfillBatch` helper unifies `GenerateMissingTitles` +
  `GenerateMissingOversight`.
- `mutatePlanningThread` helper collapses Archive/Unarchive/Activate
  handler bodies (still three external POST endpoints — those collapse
  in this spec).
- `GetTurnUsage` routed through the `withID` shim like every other
  `{id}`-bearing handler.

## Deferred work — API-shape collapses (need frontend coordination)

Each item below is one PATCH-shaped consolidation. The pattern is the
same: three or four verb-specific endpoints become one PATCH or one
endpoint with a query parameter. Always update both `ui/js/` and
`frontend/src/`, regenerate `ui/js/generated/routes.js`, and migrate
the test files that reference the old routes.

### 1. Planning-thread PATCH

Today:

- `POST /api/planning/threads/{id}/archive`
- `POST /api/planning/threads/{id}/unarchive`
- `POST /api/planning/threads/{id}/activate`

Target: `PATCH /api/planning/threads/{id}` with body `{state:
"archived" | "active" | "visible"}`.

Backend handlers already share a `mutatePlanningThread` helper, so the
backend collapse is mechanical. Frontend call sites:

- `frontend/src/stores/planning.ts:197,212`
- `frontend/src/components/plan/PlanningChatPanel.vue:436,475,504`
- `ui/js/spec-mode.js:597,617`
- `ui/js/planning-chat.js:336,388,425,456,516`
- Tests: `ui/js/tests/planning-chat-coverage.test.js`,
  `planning-chat.test.js`, `spec-mode.test.js`

### 2. Spec-action collapse

Today:

- `POST /api/specs/dispatch`
- `POST /api/specs/undispatch`
- `POST /api/specs/archive`
- `POST /api/specs/unarchive`

Each takes a `paths` array. Target: one `POST /api/specs/transition`
with `{paths, action: "dispatch" | "undispatch" | "archive" |
"unarchive"}`, or a PATCH-like shape that mirrors how spec lifecycle
already nests. Each handler has different internal logic (dispatch
creates tasks; archive walks descendants and writes a git commit) so
this is a **wrapper** consolidation, not a body merge.

### 3. Oversight phase query

Today:

- `GET /api/tasks/{id}/oversight`
- `GET /api/tasks/{id}/oversight/test`

Target: `GET /api/tasks/{id}/oversight?phase=impl|test`, default
`impl`. The two handlers in `internal/handler/oversight.go` differ
only in `store.GetOversight` vs `store.GetTestOversight`; the response
shape is already different (test oversight skips the `PhaseCount`
wrapper) so this collapse may need a unified response too.

### 4. Task action endpoints → PATCH

The richest collapse. Today these eight side-effect endpoints all
live alongside the generic PATCH that already handles most status
transitions:

- `POST /api/tasks/{id}/cancel`
- `POST /api/tasks/{id}/done` (CompleteTask)
- `POST /api/tasks/{id}/resume`
- `POST /api/tasks/{id}/restore`
- `POST /api/tasks/{id}/archive`
- `POST /api/tasks/{id}/unarchive`
- `POST /api/tasks/{id}/sync`
- `POST /api/tasks/{id}/test`

Of these, `archive`, `unarchive`, `cancel`, `restore` are pure state
transitions and could fold into PATCH (`{archived: true|false}`,
`{status: "cancelled"}`, `{deleted: false}`). `done`, `resume`,
`sync`, `test` carry richer side effects (commit pipeline, runner
restart, rebase, test agent launch) — they're fine to keep as
dedicated endpoints, but rename to make their side effect explicit
(e.g. `POST /api/tasks/{id}/commit-and-done`).

The internal helper `transitionTask(id, newStatus, opts)` mentioned in
the pass-1 analysis would centralise the diff-cache-invalidate +
event-log + thread-cascade ritual that's currently repeated across
all eight handlers.

### 5. Auth org PATCH

- `GET /api/auth/me`
- `GET /api/auth/orgs`
- `POST /api/auth/switch-org`

`switch-org` is a mutation of the "me" resource. Target: `PATCH
/api/auth/me` with `{org_id}`. Keep the GET endpoints as-is.

### 6. Ideate vs routines

`GET/POST/DELETE /api/ideate` is routine-backed (`Tags=["system:
ideation"]`). Routines are already CRUD'd via `/api/routines`. Two
options:

- **Remove the `/api/ideate` triple entirely**, surface ideation in
  the routines list with a `system:ideation` tag filter on the
  frontend.
- **Keep `/api/ideate` as a thin convenience facade** and document it
  as such — pure backend simplification with no removed surface.

The first is cleaner; the second is faster. Pick during
implementation.

## Deferred work — backend-only redundancies (pass 1 deferred)

These were known after pass 1 but skipped because the win was either
marginal or required substantial refactoring of public types.

### A. Generic `Registry[T]` for agents/flow

`internal/agents/Registry` and `internal/flow/Registry` differ only in
their value type (`Role` vs `Flow`). They share `Get`, `List`, `byKey`,
`order`, and an identical `New*` constructor pair. A generic
`internal/pkg/registry.Registry[T]` parameterised by `func(T) string
{slug}` would replace both, but every caller of `agents.Registry.Get`
and `flow.Registry.Get` would migrate to the new type.

`flow.Registry` also carries `ResolveLegacyKind`, `ResolveForTask`,
and `ResolveRoutineFlow` — three near-identical lookups that could
collapse into a `Resolve(t *store.Task, picker func(*store.Task)
string)`.

### B. CRUD handler scaffolding for agents/flow

`internal/handler/agents.go` (245 LOC) and `internal/handler/flows.go`
(283 LOC) mirror each other handler-for-handler. Every method follows
the same shape:

```
decode → validate → IsBuiltin? → exists? → write user file → reload registry → respond
```

If A above lands, lift the handler scaffold into a generic
`crud.RegisterCRUD[T, Req]` that takes a registry, a validator, a
writer, and a reloader.

### C. Cache wrappers

`internal/handler/diffcache.go` (67 LOC) and
`internal/handler/commitsbehind_cache.go` (68 LOC) are thin wrappers
over `internal/pkg/cache.TTLCache`. They survive because they provide
typed APIs; removing them would push type ambiguity to every caller.
Either keep them as-is (current pass-1 decision) or fold their bodies
into the only callers (single-callsite cases especially) and delete
the files.

### D. `httpjson.Error` helper + mass migration

The codebase mixes `http.Error(w, msg, status)` (plain text, 361
sites) with `httpjson.Write(w, code, map[string]string{"error": msg})`
(JSON, ~30 sites). Adding `httpjson.Error(w, status, msg)` and
migrating all 361 sites would unify the error envelope so clients
always parse JSON. Big migration, modest payoff — defer until there's
a frontend reason to need structured errors.

### E. Background goroutine launcher

`r.GenerateOversightBackground`, `r.GenerateTitleBackground`,
`r.SyncWorktreesBackground`, `r.RunBackground` (`internal/runner/
runner.go`) each wrap one line: `r.backgroundWg.Go("<label>:"+
taskID.String()[:8], func() { ... })`. Could become a single
generic helper `r.taskBackground(label string, taskID uuid.UUID, fn
func())`. Marginal LOC win but unifies the label-prefix convention.

## New findings — pass 2

A second look surfaced these patterns that pass 1 missed.

### F. `TaskUsage` accumulation duplication and undercount

`internal/handler/usage.go:24-30` defines `addUsage(dst, src
*TaskUsage)` that sums all five fields (`InputTokens`, `OutputTokens`,
`CacheReadInputTokens`, `CacheCreationTokens`, `CostUSD`).

`internal/handler/stats.go` accumulates the same kind of data into
its own `UsageStat` type but inlines field-by-field `+=` at ~7 call
sites and **drops `CacheReadInputTokens` and `CacheCreationTokens`**
for most buckets (`ByStatus`, `ByActivity`, `ByFailureCategory`,
`DayStat`). That undercounts cache token usage in the stats response —
correctness issue, not just duplication.

`internal/store/tasks_update.go:197-216` (`AccumulateSubAgentUsage`)
also inlines all five fields twice.

Action: move `TaskUsage.Add(other)` onto the `store.TaskUsage` type
(or an `internal/pkg/usage` package), embed `TaskUsage` inside
`UsageStat` / `DayStat` so the Add method works directly, and replace
every inline `+=` block. Fix the stats undercount as part of the
move.

### G. Bespoke JSON decode holdouts

`internal/pkg/httpjson.DecodeBody` / `DecodeOptionalBody` is in 41
sites. Five sites still hand-roll `json.NewDecoder(r.Body).Decode`:

- `internal/handler/orgs.go:160` (SwitchOrg request body)
- `internal/handler/templates.go:49` (file load, not a request — OK)
- `internal/handler/terminal.go:405` (WebSocket message, not HTTP — OK)
- `internal/handler/planning_directive.go:195` (parses a string from a
  Claude message, not a request — OK)
- `internal/handler/planning_undo.go:390,408` (parses store event
  bytes — OK)

Only `orgs.go:160` is a real candidate; the rest aren't HTTP request
bodies. One-line fix.

### H. UUID path parsing variants

There are now three patterns:

- `withID` shim in `internal/cli/server.go:917` (production).
- `parsePathID(w, r, name)` in `internal/handler/routines.go:282`
  (configurable param name).
- `uuid.Parse(r.PathValue("id"))` inline (a handful of leftover
  sites).

`parsePathID` does almost the same job as `withID` but supports
arbitrary param names. Promote it to `internal/pkg/httpjson.PathUUID`
(or similar) and have `withID` call it; then migrate any inline sites.

### I. Duplicate `bearerToken` helper

- `internal/auth/middleware.go:140` — extracts from `*http.Request`,
  used by `Auth` and `OptionalAuth`.
- `internal/handler/sandbox_proxy.go:278` — takes a raw header string,
  used by `requireClaims`.

Both check the same `Bearer ` prefix and trim. Move one canonical
helper to `internal/auth` (or a small `internal/pkg/bearer` package),
have both consumers use it.

### J. Trivial `slices.Contains` open-codes

- `internal/handler/sandbox_proxy.go:287-302` — `hasScope` and
  `hasAud` are 6-line loops that are exactly `slices.Contains`.
- `internal/handler/git.go:691` — flagged by gopls' `slicescontains`
  hint.

Drop the helpers, call `slices.Contains` directly.

### K. `Auth` vs `OptionalAuth` middleware

`internal/auth/middleware.go:66-109` — the two helpers differ only in
what they do when the bearer token is missing or fails to validate
(call `next` vs `writeUnauthorized`). Could parameterise with an
`onFail` callback, but the indirection probably hurts more than the
deduplication helps. **Marginal — likely skip.**

### L. Handler `cascade*` helpers form a family

`cascadeArchiveThreadsForTask`, `cascadeUnarchiveThreadsForTask`,
`cascadeCancelRoutineChildren`, `cascadeDisableRoutineIfLastLive
Instance` are all "do a side-effect for related entities when this
task transitions". They aren't perfectly identical but share the
guard-then-cascade shape. Could group into a `cascade` substructure
on Handler if more cascade rules are added. **Watch this space.**

## Out of scope

- The `internal/runner` 27k-LOC mountain. Worth its own audit, but
  not this spec.
- The `internal/store` per-field update methods (~25 `UpdateTaskX`
  functions, each one a `mutateTask` closure with one field write).
  Each is a typed setter; a generic `UpdateTaskFields(partial)` would
  trade type safety for line count. Leave alone.

## How to dispatch

The deferred items in **API-shape collapses** are best done as one
multi-commit task ("collapse N verb endpoints across both fronts").
The items in **backend-only redundancies** are independent and can
each be a small task. The new findings in **pass 2** are
day-of-cleanup items — pair with whoever next touches the
neighbouring file.

When ready, break this spec into per-item tasks and dispatch
individually rather than treating it as a single xlarge unit. Each
backend-only item is ≤200 LOC; each API-shape collapse is 5–10 files
across backend + both frontends + tests.
