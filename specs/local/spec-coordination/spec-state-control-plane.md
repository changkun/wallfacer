---
title: Spec State Control Plane
status: drafted
depends_on:
  - specs/local/spec-coordination.md
  - specs/local/spec-coordination/spec-archival.md
affects:
  - internal/spec/
  - internal/handler/specs.go
  - internal/handler/specs_dispatch.go
  - internal/handler/planning.go
  - internal/handler/planning_git.go
  - internal/handler/tasks.go
  - internal/cli/server.go
  - internal/runner/drift.go
  - internal/runner/oversight.go
  - internal/store/
  - ui/js/spec-explorer.js
  - ui/js/spec-mode.js
created: 2026-03-29
updated: 2026-04-12
author: changkun
dispatched_task_id: null
effort: large
---

# Spec State Control Plane

The wallfacer server owns every state transition a spec goes through. The
lifecycle state machine lives in `internal/spec/lifecycle.go`, but today
only two classes of transitions are automated:

- **To `archived` and back to `drafted`** (via the archival endpoints in
  `internal/handler/specs.go` — shipped by spec-archival.md).
- **To `complete` on task done** (via `SpecCompletionHook` wired to
  `store.OnDone` in `internal/cli/server.go`).

Every other transition happens by hand: the user edits frontmatter directly,
or an agent writes `status: validated` during `/wf-spec-refine`. There is no
automated `drafted → validated`, no `validated` marking on dispatch, and no
propagation of downstream staleness when upstream specs change. **Drift
detection does not exist at all** — `SpecCompletionHook` writes `complete`
unconditionally, no tester-in-the-loop verdict, no comparison against the
spec's intent.

This spec establishes the **spec state control plane**: server-managed hooks
that move specs through the lifecycle in response to the events that justify
each transition. Drift assessment is the decision gate of the task-completion
hook: implementation done → tester verdict → `complete` or `stale`.

---

## Current State

Already in place, no change needed:

- **`internal/spec/lifecycle.go`** — six-state machine with every legal edge
  (`spec-document-model.md`, `spec-archival/core-model.md`).
- **`internal/spec/write.go` `UpdateFrontmatter`** — atomic YAML-field write,
  used by dispatch, archive, undispatch, and the completion hook.
- **`SpecCompletionHook`** (`internal/handler/specs_dispatch.go`) — called by
  `store.OnDone` when a task reaches `done`; writes `status: complete`
  unconditionally.
- **Task test action** — `POST /api/tasks/{id}/test` (`TestTask` handler)
  runs a test-verification agent against task worktrees. This is the
  infrastructure Priority 3 uses to produce a drift verdict.
- **Archive / unarchive endpoints** (`internal/handler/specs.go`) — archived
  specs are exempt from every propagation rule in this spec.
- **Undispatch** writes `status: validated` when clearing `dispatched_task_id`.

Gaps this spec closes, in priority order:

| Gap | Consequence |
|---|---|
| Dispatch does not set `validated` | Status lies about readiness during execution |
| Chat edits do not fan out | Downstream specs drift silently after upstream edits |
| Task-done writes `complete` blindly | No drift assessment; `complete` can mean "diverged from intent" |
| Downstream dependents not notified on completion | No review signal when a dependency lands with drift |

---

## Design

A control-plane hook is a server-side function that runs in response to a
specific event, reads the spec tree, and writes one or more frontmatter
mutations. Every hook follows the same shape:

1. Triggered by an existing server event (task state change, planning
   commit, dispatch call).
2. Reads the affected spec plus its neighbourhood from the on-disk tree
   (`spec.BuildTree` / `spec.Adjacency`). Skips archived specs at every
   step — `Adjacency` already prunes them.
3. Validates each proposed transition via `spec.StatusMachine.Validate`.
   Illegal transitions are logged and skipped, never applied.
4. Writes via `spec.UpdateFrontmatter` — one spec at a time, in a loop;
   no transaction. Partial application is acceptable because every hook
   is idempotent.
5. Commits the batch via `commitSpecChanges` (in `specs.go`) so the
   transition is visible in git and reversible with `git revert` — same
   pattern archive/unarchive uses.

All priorities below obey these rules.

---

## Priority 1 — Chat-edit fan-out

**Trigger:** a planning chat round commits edits under `specs/`
(`commitPlanningRound` in `internal/handler/planning_git.go`). The commit
itself already runs; extract the set of modified spec paths from
`git diff --name-only HEAD^ HEAD`.

**Action:** for each spec the round modified, compute its reverse-dependency
set (`dag.ReverseEdges(Adjacency(tree))`) and transition every non-archived
dependent toward `stale`. `Adjacency` already omits archived sources/sinks.

**Drift assessment on chat edits.** The chat edit is a design change on
the upstream spec; dependents may or may not still hold. In the absence of
a tester for chat-driven design changes, we fall back to the blunt but
correct rule: mark every live dependent `stale`. The user clears the
staleness with `/wf-spec-refine`, which either re-validates (edit was
compatible) or updates the dependent (edit was breaking). This matches how
specs today become stale via manual edits — we are just automating the
detection.

A smarter variant (deferred) would run a semantic drift assessment on each
dependent, same agent the Priority 3 tester uses, and only mark stale
those that actually diverge. Worth revisiting after Priority 3 lands and
the tester agent is known-good.

**Skip conditions:**
- Dependent is `archived` — handled by `Adjacency`.
- Dependent is already `stale` — same-to-same rejected by `StatusMachine`.
- Source spec's edit was only trailing-whitespace / frontmatter `updated`
  bump: skip (no semantic change). Heuristic: compare the pre/post commit
  bodies after stripping the frontmatter `updated` line.

**Commit:** fold the status writes into the same planning-round commit so
`git revert` of the planning round reverses the staleness cascade. Insert
the fan-out between "detect modified specs" and the final commit call.

**Files touched:**
- `internal/handler/planning_git.go` — add fan-out helper; call before the
  final commit
- Shared fan-out helper (with Priority 3) in `internal/spec/` or
  `internal/handler/`

**Effort:** small.

---

## Priority 2 — Dispatch sets `validated`

**Trigger:** `DispatchSpecs` in `internal/handler/specs_dispatch.go`. The
handler already writes `dispatched_task_id` but leaves `status` at whatever
it was.

**Action:** after writing `dispatched_task_id`, write `status: validated`
for each dispatched spec in the same `UpdateFrontmatter` call. The
pre-dispatch guard already rejects specs not at `validated` (line 85), so
this is idempotent on the leaf path. The real value is the **folder
dispatch** case: extend `DispatchSpecs` to accept a non-leaf input, dispatch
every leaf in the subtree, and mark every spec in the subtree `validated`.

**Non-leaf dispatch (open question).** Dispatch currently rejects non-leaf
paths with "non-leaf specs cannot be dispatched". Lifting that restriction
is a meaningful behavioural change; the user's framing ("task or a complete
spec folder is dispatched") calls for it explicitly. Ship the restriction
lift together with the `validated` write.

**Skip conditions:** archived specs already rejected by the dispatch
pre-check. Specs in a dispatched subtree that are `complete` or `stale`
are out of scope — only `drafted` / `validated` candidates flip.

**Files touched:**
- `internal/handler/specs_dispatch.go` — extend the frontmatter update map;
  extend `DispatchSpecs` to walk non-leaf subtrees

**Effort:** medium.

---

## Priority 3 — Task done: tester-mediated drift check and fan-out

**The pressing piece.** `SpecCompletionHook` today writes `status: complete`
the moment a task's status reaches `done`. This is wrong: a task can be
done and still have implemented something that diverges from the spec's
intent. The spec should transition to `complete` only when a tester — the
same agent the "Test" button already invokes — verifies the implementation
matches.

**Trigger:** same as today — `store.OnDone` fires `SpecCompletionHook` in
a background goroutine when a dispatched task reaches `done`.

**New flow:**

1. **Hold at implementation-done.** The hook no longer writes `complete`
   immediately. Instead, it records the task's commit range on the spec
   (e.g., in a new `implementation_commit: <sha>..<sha>` frontmatter
   field) and keeps the spec's status at `validated`. The spec is in
   **testing** conceptually — the task is done on disk but the spec
   has not been certified.

2. **Launch the tester.** The hook calls the existing test-verification
   agent (the same sandbox path as `POST /api/tasks/{id}/test`). The
   tester receives the spec body, the task's git diff, and the acceptance
   criteria. It emits a structured drift verdict:

   ```
   {
     "expected_files": ["runner.go", "execute.go"],
     "actual_files":   ["runner.go", "execute.go", "container.go"],
     "unexpected":     ["container.go"],
     "missing":        [],
     "criteria":       {"satisfied": 5, "diverged": 1, "total": 6},
     "drift_level":    "moderate"
   }
   ```

3. **Decide.** Based on the verdict:
   - **Minimal drift** (>90% criteria satisfied, no unexpected files of
     substance) → `validated → complete`, no fan-out beyond normal.
   - **Moderate drift** (70–90% satisfied) → `validated → complete`,
     drift report attached as `## Outcome` section, **fan out to
     downstream dependents as `stale`** (they need to review against the
     actual implementation, not the spec).
   - **Significant drift** (<70% satisfied) → `validated → stale`
     directly. Task can be re-dispatched after `/wf-spec-refine`.

4. **Fan out.** For moderate/significant drift, transition every
   non-archived dependent to `stale` using the same shared helper as
   Priority 1.

5. **Commit.** Stage the modified spec(s) and commit with subject
   `<path>: mark complete` or `<path>: mark stale (drift: <level>)`. Git
   revert reverses the verdict and the fan-out together.

**Why the tester, not a separate drift module.** The test agent already
exists, has a sandbox, and produces structured oversight output. Drift
assessment is its job: "does the implementation match what the spec
asked for?" is the same question as "does the test pass?" phrased
differently. Reusing this path means no new agent, no new container
image, and the existing oversight UI can render drift verdicts.

**Testing-state visibility.** The spec explorer should show a spec as
"in testing" while the tester is running — a distinct affordance from
plain `validated`. Options:
- Add a 7th lifecycle state `testing` between `validated` and
  `complete`/`stale`. Cleanest but requires state-machine and UI updates.
- Track testing-in-progress via the task status (`waiting` / oversight
  in-flight) without changing the spec's own status. No lifecycle churn.
- Add a non-status `testing: true` frontmatter flag rendered as a badge.
  Cheap; easy to forget to clear.

Tentative: use the task status (option 2) — a test run in progress is
observable from `store.Task` state, no new spec state needed. Revisit
if users find it invisible.

**Skip conditions:**
- Archived specs — dispatch already rejected them; defensive guard if
  state somehow leaks.
- Tester failure (agent crash, timeout) — fall back to today's behaviour
  (write `complete`) and log a warning. Never block completion on tester
  availability.

**Files touched:**
- `internal/handler/specs_dispatch.go` — extend `SpecCompletionHook`:
  launch tester, interpret verdict, decide status, run fan-out
- `internal/runner/drift.go` (new) — deterministic file-level drift
  (spec `affects` vs task diff)
- `internal/runner/oversight.go` — extend the test-verification agent to
  emit the drift verdict schema (or add a new agent alongside it)
- `internal/store/` — `SaveDriftReport(taskID, report)` /
  `GetDriftReport(taskID)` persist drift reports
- Shared fan-out helper (with Priority 1) in `internal/spec/` or
  `internal/handler/`

**Effort:** large (this is where most of the work lives).

---

## Priority 4 — Cross-tree staleness (periodic scan)

Complementary to the event-driven hooks: an `affects`-based scan catches
drift that slipped in via commits outside the spec flow (manual edits,
refactors, rebases).

**Trigger:** workspace load; manual refresh from the UI; optional cron.

**Action:** for each non-archived `complete` spec, check whether any file
in `affects` has been modified since the spec's `updated` (`git log --since`).
If so, surface a stale badge in the explorer — do **not** auto-mutate the
status. This is advisory; the user decides whether the change is material
and runs `/wf-spec-refine` or `/wf-spec-diff`.

Archived specs skipped entirely.

**Effort:** small.

---

## Archived Specs Are Fully Excluded

Archived specs are invisible to every channel in this spec — same invariant
`internal/spec/impact.go`, `progress.go`, and `validate.go` enforce:

| Hook | Archive skip rule |
|---|---|
| P1 — Chat fan-out | `Adjacency` already omits archived sources/sinks |
| P2 — Dispatch | Dispatch rejects archived specs pre-write |
| P3 — Task done (tester + fan-out) | `Adjacency` handles fan-out; the completing spec itself can't be archived (dispatch rejected it) |
| P4 — Periodic scan | Skip archived specs; their `affects` are not checked |

---

## Propagation Rules

Drift propagates through two channels:

### Through the filesystem tree (upward)

Leaf drift bubbles up to non-archived ancestors. When P3 writes `stale` (or
moderate drift) on a leaf, the ancestors get advisory indicators:

- Any leaf `stale` in the subtree → nearest non-archived ancestor gets
  **"drift: review required"**
- 2+ leaves with moderate drift → nearest non-archived ancestor gets
  **"drift: review suggested"**

Propagation stops at the first archived ancestor.

### Along the dependency DAG (forward)

Covered by the fan-out in P1 and P3: dependents of a drifted spec move to
`stale`. `Adjacency` already enforces that archived dependents don't
receive propagation.

---

## The `affects` Field

The `affects` field maps specs to code — the edge drift uses to connect
commits to specs:

```yaml
affects:
  - internal/sandbox/
  - internal/runner/execute.go
  - internal/runner/container.go
```

Used by:
- P3 — file-level drift: compare spec `affects` vs the task's actual diff
  to flag unexpected / missing files
- P4 — periodic scan: check whether any `affects` file has changed since
  the spec's `updated`

Archived specs' `affects` are not checked anywhere.

Bootstrap: populate manually at current scale. As spec count grows, the
agent proposes `affects` during spec creation and validates against diffs.

---

## UI

Priorities 1 and 2 drive the existing `stale` and `validated` status
badges; no new UI.

Priority 3 surfaces drift verdicts:

```
specs/
  ✅ sandbox-backends.md          ⚠ drift detected
  ✅ storage-backends.md
  ✔ container-reuse.md           ⚠ upstream drift (sandbox-backends)
  📝 k8s-sandbox.md
  📦 archived-spec.md            (no badge — archived)
```

The `⚠` icon already exists in `ui/js/spec-explorer.js` as `stale`; drift
badges reuse the same visual treatment. Archived specs never get a drift
badge.

Focused view inline warning on dependents of a drifted spec:

```
⚠ This spec may be stale. sandbox-backends.md (which this spec depends on)
  completed with implementation drift. Review assumptions before dispatching.
  [Review Changes] [Dismiss]
```

Priority 4 reuses the same badge.

---

## Implementation Index

| File | Priority | Change |
|---|:-:|---|
| `internal/handler/planning_git.go` | P1 | Chat-edit fan-out; call before final commit |
| `internal/handler/specs_dispatch.go` | P2, P3 | Dispatch writes `validated`; non-leaf dispatch; `SpecCompletionHook` launches tester and branches on drift verdict |
| `internal/runner/drift.go` (new) | P3 | `CheckTaskDrift(taskID, spec) → DriftReport` (file-level deterministic) |
| `internal/runner/oversight.go` | P3 | Extend or alias test-verification agent to emit drift verdict schema |
| `internal/store/` | P3 | `SaveDriftReport` / `GetDriftReport`; persist alongside task data |
| `internal/handler/specs.go` | P3 | `GET /api/specs/{path}/drift` — return the drift report |
| `internal/handler/explorer.go` | P3 | Propagate drift indicators to non-archived ancestors in tree response |
| `internal/handler/tasks.go` | P4 | Workspace-load periodic scan hook |
| Shared fan-out helper | P1, P3 | `FanOutStale(tree, completedPath) error` in `internal/spec/` |
| `ui/js/spec-explorer.js` | P3, P4 | Render drift indicators (reuses `stale` icon); suppress for archived |
| `ui/js/spec-mode.js` | P3 | Inline drift warning in focused view with "Refine" / "Accept" |

---

## Acceptance Criteria

### Priority 1 — Chat fan-out
- After a planning chat round commits changes to spec files, every
  non-archived spec whose `depends_on` includes any modified spec
  transitions to `stale`.
- The status writes are part of the same planning commit (so `git revert`
  reverses the cascade in one step).
- Unit test: plan a chat round that modifies `A.md` where `B.md` and
  `C.md` depend on `A`; assert both move to `stale`; assert `B` stays
  untouched if it is `archived`.

### Priority 2 — Dispatch → validated
- Dispatching a `drafted` spec writes `status: validated` along with
  `dispatched_task_id`.
- Dispatching a non-leaf marks every leaf in the subtree `validated` and
  creates a kanban task per leaf.
- Unit test: dispatch a `drafted` spec; assert it reads `validated` after
  the handler returns.

### Priority 3 — Task done + tester + fan-out
- When a dispatched task reaches `done`, `SpecCompletionHook` launches
  the tester instead of writing `complete` immediately.
- The tester emits a drift verdict (`minimal` / `moderate` / `significant`).
- Status transitions match the verdict: minimal → `complete`, moderate →
  `complete` + fan-out, significant → `stale`.
- On moderate or significant drift, every non-archived dependent moves
  to `stale`.
- Drift report persisted and accessible via
  `GET /api/specs/{path}/drift`.
- The status writes are committed; `git revert` reverses the verdict
  and the cascade together.
- Unit tests cover all three drift levels.
- Integration test: full round-trip from `store.OnDone` → tester →
  `complete` for a minimal-drift case.

### Priority 4 — Periodic scan
- On workspace load, non-archived `complete` specs whose `affects` files
  changed since `updated` receive a stale badge in the explorer.
- Scan does not mutate frontmatter — advisory only.
- Unit test: write a spec with `affects: [foo.go]`, commit a change to
  `foo.go` dated after the spec's `updated`; scan flags the spec.

---

## Open Questions

1. **"Testing" as a lifecycle state.** Should the P3 hold-between-done-and-verdict
   introduce a 7th `testing` status, or stay implicit via task state? Tentative:
   implicit — use task state during the verdict phase; revisit if users find
   the transient state invisible.
2. **Non-leaf dispatch** (P2) is in scope here. Reviewers may prefer to split
   it into its own spec. Tentative: keep together — the `validated` write on
   dispatch is what makes folder dispatch meaningful.
3. **Chat-edit fan-out granularity** (P1). The blunt "mark every live dependent
   `stale`" rule is easy to implement but noisy. A semantic-drift pass
   (same agent as P3) on each dependent would be more precise. Tentative:
   ship the blunt rule first; layer semantic assessment once P3's tester is
   known-good.
4. **Tester failure handling** (P3). If the tester crashes or times out, what
   should the spec status be? Tentative: fall back to today's behaviour
   (write `complete`) with a logged warning — never block completion on
   tester availability. Alternative: hold at `validated` pending retry.
5. **Drift report storage location** (P3). Inline in the spec body as an
   `## Outcome` section (visible in the focused view), in a sidecar file
   under `.wallfacer/drift/<spec-path>.json`, or both? Tentative: inline
   outcome for user-visible summary; sidecar for full structured data the
   UI can deep-link to.

