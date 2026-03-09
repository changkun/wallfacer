# Oversight Card Panel

## Overview

The oversight mini-panel is a collapsible `<details>` element rendered on the
Kanban card for `done` and `failed` tasks that have an execution trail and a
`ready` oversight summary.  It shows a phase-count badge in the summary line
and, when expanded, a list of oversight phases fetched from
`/api/tasks/{id}/oversight`.

## Circular-dependency fix

`render.js` computes `showOversight` from `cardOversightCache.get(t.id)`.  The
cache was originally populated only inside the `toggle` handler on the
`<details>` element, which itself is only rendered when `showOversight` is
already `true` — a permanently-unsatisfiable circular dependency.

The fix seeds `cardOversightCache` from every place that already fetches
`/api/tasks/{id}/oversight`:

| File | Function | When cache is written |
|------|----------|-----------------------|
| `ui/js/modal-oversight.js` | `renderOversightInLogs()` | `.then()` callback after the modal fetches oversight; only when `data.status === 'ready'` and `data.phase_count != null` |
| `ui/js/modal-logs.js` | `startLogStream()` | Pre-fetch `.then()` at modal open; same guard |
| `ui/js/modal-logs.js` | `startImplLogFetch()` | Pre-fetch `.then()` at modal open; same guard |

`renderTestOversightInTestLogs()` (test-run oversight endpoint
`/oversight/test`) is intentionally **not** included — the test-run oversight
is separate from the implementation oversight and must not overwrite the card
cache.

After writing to the cache, each location calls `scheduleRender()` so the
board re-renders and `showOversight` evaluates to `true` for the affected card.

## Behavior specification

### (a) Done task with oversight=ready — badge appears after modal opened once

**Precondition:** A task with `status === 'done'`, at least one turn (`turns >
0`), and `/api/tasks/{id}/oversight` returning `{ status: "ready",
phase_count: N, phases: [...] }`.

**Steps:**
1. Board renders — card has no oversight badge (cache empty).
2. User opens the task detail modal.
3. `startLogStream()` / `startImplLogFetch()` fires a pre-fetch of the
   oversight endpoint.
4. The `.then()` callback populates `cardOversightCache` with
   `{ phase_count: N, phases: [...] }` and calls `scheduleRender()`.
5. Board re-renders — `showOversight` is now `true` for the card.
6. Card displays `<details class="card-oversight">` with summary text
   `"N phases"`.
7. User closes the modal.
8. Board re-renders again — badge is still visible (cache is not cleared on
   modal close).

**Expected:** Phase-count badge (`"N phases"`) is visible on the card after
step 6 at the latest.

### (b) Done task with oversight=pending — no badge shown

**Precondition:** Same as (a) but the oversight endpoint returns
`{ status: "pending" }` (generation has not started yet).

**Expected:** `cardOversightCache` is never written for this task (the `ready`
guard is not satisfied), so `showOversight` remains `false` and no badge
appears on the card even after the modal is opened.

### (c) Failed task with no turns — no badge shown

**Precondition:** A task with `status === 'failed'`, `turns === 0`, no
`result`, no `stop_reason`.

**Expected:** `hasExecutionTrail(t)` returns `false`, so `showOversight` is
`false` regardless of the cache content.  No badge appears.

## Data flow

```
Modal open
  └─ startLogStream / startImplLogFetch
       └─ _modalApiJson('/api/tasks/{id}/oversight')
            └─ .then(data)
                 ├─ oversightData = data          (for modal rendering)
                 └─ if ready & phase_count != null
                      ├─ cardOversightCache.set(id, {...})
                      └─ scheduleRender()
                           └─ renderCard()
                                └─ showOversight = true → renders <details>

Modal opened once → subsequent board renders keep badge visible via cache
```

## Cache lifetime

`cardOversightCache` is a module-level `Map` in `render.js`.  It is never
explicitly cleared — entries survive for the lifetime of the page.  This
matches the intended behavior: oversight data for a `done`/`failed` task is
immutable once generated.
