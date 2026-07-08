---
title: Review Adversarial Verification
status: archived
depends_on: []
affects:
  - internal/adversarial/review.go
  - internal/adversarial/noop.go
  - internal/adversarial/harness_critic.go
  - internal/adversarial/session_proposer.go
  - internal/store/models.go
  - internal/store/tasks_create_delete.go
  - internal/store/tasks_update.go
  - internal/handler/handler.go
  - internal/handler/tasks_autopilot.go
  - internal/handler/execute.go
  - internal/handler/config.go
  - go.mod
  - frontend/src/api/types.ts
  - frontend/src/components/TaskDetail.vue
effort: large
created: 2026-06-26
updated: 2026-06-28
author: changkun
dispatched_task_id: null
---

# Review Adversarial Verification

## Revision 2026-06-26: review outcome, hybrid execution model

The first implementation shipped (commits `67d055fd`…`5958313e`) but a review
found design defects. This revision records the corrected design. Where it
conflicts with the original "## Design" section below, **this section wins**.

### Defects found in the shipped v1

1. **Runaway auto-runs.** `tryAutoReview` fired a goroutine per waiting task every
   tick. `ReviewUnresolved` is only written when a run *finishes* (minutes), so the
   task stayed a candidate and was re-launched every tick — unbounded duplicate
   runs sharing one `.review` dir; an erroring run that never marks retried forever
   with no breaker and no concurrency cap.
2. **`.review` written inside the worktree.** `StateDir = cwd/.review` is under the
   tree that `git add -A` commits, so debate scratch leaks into the PR branch and
   pollutes the next `generateWorktreeDiff`.
3. **Critic is blind to the code.** `HarnessCritic` drops `CriticInput.Cwd` and
   `RunCriticRound` runs in a base container with no worktree mount — the critic
   sees only the diff *patch text*, can't read neighbouring files or run tests.
4. **Critics aren't diverse.** The `NewCritic` factory ignores `forkIdx` and
   returns the same Claude harness — two samples of one model, not perspectives.
5. **Proposer runs unrestricted on the host.** review's `ClaudeProposer` execs
   `claude --resume --fork-session` on the host in the *live* worktree with no
   tool restriction → it can edit the user's tree mid-debate; auto-submit commits
   the edits.
6. **Untracked cost, wrong-store writes, stubbed criteria, permanent sentinel,
   dead `NoopVerifier`, non-persisted toggle** — see review notes.

### Decided execution model (hybrid)

`claude --resume` is **cwd-scoped** (review spec 17: `ErrCwdMismatch` on cwd
mismatch). The fork-session proposer therefore *must* run in the original task
worktree; a fully-isolated tree would break it. The critic is stateless
`claude -p` with no cwd constraint, so it can run anywhere. Hence:

| Actor | Where | Tools | Why |
|-------|-------|-------|-----|
| Proposer (fork-session) | original worktree | **read-only** | cwd-scoping forces the original path; read-only removes mutation/commit risk |
| Critics (stateless, diverse harnesses) | **throwaway `git worktree` at task HEAD** | read + build + test | no cwd constraint → isolated tree, can run tests, zero risk to the real tree |
| `.review` state | wallfacer data dir, keyed by task ID | — | never inside a committed tree |

Quality is the priority: if review runs at all, the critic gets real code access
and the forks use diverse harnesses (e.g. fork 1 Claude, fork 2 Codex). Token
cost is managed by *scope and budget* (`CostCapTokens`, `maxConcurrentReview`,
opt-in / diff-size gating), not by weakening the critic.

### Work breakdown (this revision)

- [x] **Dedup + concurrency cap.** `beginReview`/`endReview` in-flight set guarded by
  `reviewMu`, `maxConcurrentReview=2`, reserved before goroutine spawn; manual
  trigger shares the guard (409 when in flight). _(commit: dedup/cap)_
- [x] **Circuit breaker + store-safe writes.** `"auto-review"` breaker; capture the
  owning `*store.Store` at scan time; persist only when the task is still
  `waiting`. _(commit: harden auto-review)_
- [x] **`.review` relocation + deterministic cwd.** `StateDir` →
  `<worktreesDir>/<taskID>/.review` (beside the worktree, never committed);
  `primaryWorktree` picks a stable cwd. _(commit: place review state beside the
  worktree)_
- [x] **Critic code access (read) + per-fork diversity.** `HarnessCritic` threads
  `Cwd` (→ `RunCriticRound` sets the agent `WorkDir`) so the critic reads the
  full codebase, not just the patch; `ReviewVerifier` rotates critic harnesses
  (Claude/Codex) by `forkIdx`. _(commit: run critics in the worktree…)_
- [x] **Critics run in a throwaway worktree (test-capable, isolated).**
  Correction to the prior item: wallfacer's claude harness runs agents with
  `--dangerously-skip-permissions`, so the critic is *not* read-only — pointing
  it at the real worktree gave it write access. `ReviewVerifier` now creates a
  throwaway `git worktree` at HEAD (`newCriticWorktree`) and runs all critics
  there; they can build/run tests with zero risk to the task branch. The
  proposer keeps the real worktree (fork-session is cwd-scoped). _(commit: run
  critics in a throwaway worktree)_
- [x] **review: read-only proposer option** (review repo, spec 38, released v0.1.1).
  `claude.WithProposerReadOnly()` bars Write/Edit/MultiEdit/NotebookEdit/Bash so
  the fork-session proposer (which shares the real worktree) can argue/concede
  but never edit it. Wired into `NewSessionProposer` and live (go.mod bumped to
  review v0.1.1). _(commit: restrict the fork-session proposer to read-only tools)_
- [x] **Usage attribution (complete: cost + tokens).** `runReview` attributes the
  full review spend to the task under `SandboxActivityReview`: cost from
  `VerifyResult.USD`, and the input/output/cache token breakdown read from
  review's session `end.json` (`VerifyResult.SessionDir`), which review already
  writes with the fork-session proposer + critics combined. Two parts: critics
  now report their usage back to review (else its totals were proposer-only), and
  wallfacer reads the artifact. **No review release needed.** _(commits: report
  critic token usage; attribute complete review token usage)_
- [x] **Config knobs (env).** `WALLFACER_REVIEW_FORKS` / `_ROUNDS` / `_COST_CAP`
  override the defaults via `reviewTuning`; documented in the configuration guide.
  _(commit: make fork count, rounds, and cost cap configurable)_. A `/api/config`
  + frontend surface is a later nicety.
- [x] **Reset `ReviewUnresolved` on resume.** `ClearReviewResult` is called from
  both resume paths (feedback/auto-resume and manual `ResumeTask`), alongside the
  feedback path's existing test-verdict reset, so a resumed task is re-verified
  once it returns to waiting. _(commit: clear the review verdict on resume)_
- [x] **Criteria threading (goal #7).** Unblocked and wired: landed
  `Task.Criteria` ([[test-criteria]] Phases 1–2) and `runReview` now passes
  `t.Criteria` into `VerifyInput`, anchoring critics to the same acceptance bar
  as the test agent. _(commits: persist Task.Criteria; wire criteria through
  handlers and into test + review paths)_

## Problem

After the implementation agent finishes a task, wallfacer's only automated
verification is a single test agent. A single agent cannot find bugs it introduced
itself; it has the same blind spots as the implementation run. Adversarial
multi-agent debate — multiple critic agents attacking the changes, the proposer
rebuttng — is proven to surface issues the implementer missed.

Review (`latere.ai/x/review`, at `~/dev/latere.ai/review`) implements exactly this
protocol. Today it is a standalone CLI. Its core logic is under `internal/` and
cannot be imported by wallfacer or any other Go module. The test step can be skipped
globally; no equivalent mechanism exists for adversarial verification.

## Goal

1. Extract review's engine, interfaces, and result types into a public
   `pkg/adversarial` API (prerequisite in the review repo — see
   `~/dev/latere.ai/review/specs/37-pkg-public-api.md`).
2. Define a `Verifier` interface in `internal/adversarial/` — the wallfacer-internal
   plugin seam — so other verification strategies can plug in alongside the review
   implementation.
3. Implement `HarnessCritic`: a wallfacer-owned adapter that satisfies
   `adversarial.Critic` using the runner's existing agent-invocation path. This
   makes all five wallfacer harnesses available as critics without importing
   review's bundled claude/codex drivers.
4. Implement `SessionProposer`: a thin adapter over
   `pkg/adversarial/claude.NewProposer`, gated on `Task.SessionID != nil` (the
   session ID the runner persists after implementation).
5. Wire `tryAutoAdon` into the autopilot loop behind a toggleable flag (off by
   default), mirroring the `autotest` toggle pattern.
6. Surface unresolved attack counts and the headline dispute in the task model and
   frontend so users know when adversarial review found open issues.
7. Thread `Task.Criteria` (from [[test-criteria]]) into the verifier input as part
   of `TaskContext` so critics are anchored to the same acceptance bar as the test
   agent.

## External prerequisite

This spec **requires** `latere.ai/x/review/pkg/adversarial` to be importable first.
That extraction is specified in review `specs/37-pkg-public-api.md`. While that spec
is being implemented, wallfacer's `go.mod` should add a local `replace` directive:

```
replace latere.ai/x/review => ../../review
```

Phase 1 of this spec (the `internal/adversarial/` package) can be drafted and
tested with a stub that returns a fixed `Result{}` until the review extraction
lands; the stub is replaced in Phase 2.

## Design

### The `Verifier` interface (review-owned, not wallfacer-owned)

The `adversarial.Verifier` interface and its `VerifyInput`/`VerifyResult` types are
defined in `latere.ai/x/review/pkg/adversarial` (see review
`specs/37-pkg-public-api.md`). Wallfacer imports them; it does not redefine them.
This makes `adversarial.Verifier` the canonical, latere-wide integration seam —
any latere tool that wants to embed adversarial verification satisfies the same
interface without per-tool reinvention.

`internal/adversarial/` in wallfacer is therefore a pure implementation package:
it provides concrete types that satisfy `adversarial.Verifier` and `adversarial.Critic`,
wired to wallfacer's runner infrastructure. It defines no new interface types.

**`internal/adversarial/noop.go`**: `NoopVerifier` — satisfies `adversarial.Verifier`,
`Verify` returns `(nil, nil)` immediately. Active when review is toggled off, zero cost
and zero behavioral change for the skip path.

### HarnessCritic (`internal/adversarial/harness_critic.go`)

`HarnessCritic` satisfies `adversarial.Critic` using wallfacer's existing
`runner.runAgent` infrastructure. Each `Round` call is a one-shot stateless agent
invocation: the critic prompt is assembled from the review protocol fields
(`adversarial/critic.AssemblePrompt`), passed as stdin to the harness, and the
stdout is returned as `CriticResult.Text`. The runner's per-agent token tracking
accumulates normally.

```go
// HarnessCritic implements adversarial.Critic using a wallfacer harness.
type HarnessCritic struct {
    harness harness.Harness
    runner  *runner.Runner  // for runAgent
}

func NewHarnessCritic(h harness.Harness, r *runner.Runner) adversarial.Critic
```

`Round` maps `CriticInput` fields to `runAgentOpts`. The agent runs non-resumably
(no session ID); each fork's critic round is independent. This means Codex, Cursor,
OpenCode, Claude, and Pi can all serve as critics without any review-side driver
changes — this is the multi-harness extensibility the user asked for.

The critic harness defaults to the same harness as the task. Future configuration
can specify a different harness (e.g., use Claude as critic even when the task ran
on Codex).

### SessionProposer (`internal/adversarial/session_proposer.go`)

`SessionProposer` wraps `pkg/adversarial/claude.NewProposer` and is the implementation
used when `Task.SessionID != nil`:

```go
// NewSessionProposer returns a Proposer backed by the claude fork-session path.
// sessionID is Task.SessionID; cwd is the task's working directory.
// Returns nil if sessionID is empty — callers must check.
func NewSessionProposer(sessionID, cwd string) adversarial.Proposer
```

The proposer is **Claude-only** because `--fork-session` is a Claude-native feature.
The gate is `Task.SessionID != nil`: this field is set by the runner's
`tasks_update.go:202` call for Claude tasks; non-Claude harnesses do not emit a
session ID so the field stays nil, and `tryAutoAdon` skips those tasks in v1.

Future: add a `SnapshotProposer` that feeds the task prompt as a cold-start message
to any harness — enabling non-Claude tasks to participate in adversarial review
without fork-session, at the cost of losing the implementation agent's full context.

### ReviewVerifier (`internal/adversarial/review.go`)

`ReviewVerifier` is the concrete implementation of `adversarial.Verifier` (the review
package interface). It wires the proposer and critic into an `adversarial.Engine`
and calls `Run`:

```go
// ReviewVerifier implements latere.ai/x/review/pkg/adversarial.Verifier.
type ReviewVerifier struct {
    runner  *runner.Runner
    harness harness.Harness
}

func (v *ReviewVerifier) Verify(ctx context.Context, in adversarial.VerifyInput) (*adversarial.VerifyResult, error) {
    if in.SessionID == "" {
        return nil, nil  // proposer unavailable; skip silently
    }
    proposer := NewSessionProposer(in.SessionID, in.Cwd)
    criticFactory := func(forkIdx int) adversarial.Critic {
        return NewHarnessCritic(v.harness, v.runner)
    }
    sess, err := adversarial_state.NewSession(in.StateDir)
    if err != nil {
        return nil, err
    }
    engine := &adversarial.Engine{
        Sess:          sess,
        Cwd:           in.Cwd,
        ForkCount:     in.ForkCount,
        Proposer:      proposer,
        NewCritic:     criticFactory,
        MaxRounds:     in.MaxRounds,
        CostCap:       in.CostCapTokens,
        TaskContext:   buildTaskContext(in.TaskPrompt, in.Criteria),
        DiffPatch:     in.DiffPatch,
    }
    summary, err := engine.Run(ctx)
    if err != nil {
        return nil, err
    }
    return &adversarial.VerifyResult{
        Unresolved: summary.Unresolved,
        Headline:   summary.Headline,
        SessionDir: sess.Dir(),
        USD:        summary.USD,
    }, nil
}

// buildTaskContext merges prompt and criteria into review's TaskContext field.
func buildTaskContext(prompt, criteria string) string {
    if criteria == "" {
        return prompt
    }
    return prompt + "\n\n## Acceptance Criteria\n" + criteria
}
```

### Task model (`internal/store/models.go`)

Add three fields to the `Task` struct near `SessionID`:

```go
ReviewUnresolved *int   `json:"review_unresolved,omitempty"` // nil = not yet run
ReviewHeadline   string `json:"review_headline,omitempty"`
ReviewSessionDir string `json:"review_session_dir,omitempty"`
```

- `ReviewUnresolved == nil`: review has not run for this task (or is disabled globally).
- `ReviewUnresolved == 0`: review ran and found no unresolved attacks. Clean.
- `ReviewUnresolved > 0`: review found open disputes; `ReviewHeadline` holds the summary.

No migration needed: `omitempty` on `*int` means nil serializes to absent; existing
task JSON files deserialize with `ReviewUnresolved == nil`.

Store updates:
- `UpdateTask` (handler PATCH at `tasks_update.go`) — add setter for the three review
  fields; only the autopilot and the manual trigger write these, not user-initiated
  PATCH.

### Handler toggle (`internal/handler/handler.go`)

Add an `reviewEnabled atomic.Bool` field to `Handler`, following the `autotest` pattern
exactly:

```go
reviewEnabled atomic.Bool   // in Handler struct, near autotest
```

```go
func (h *Handler) ReviewEnabled() bool     { return h.reviewEnabled.Load() }
func (h *Handler) SetReview(enabled bool)  { h.reviewEnabled.Store(enabled) }
```

Default: false. Surfaces in `GET /api/config` response (`"review": false`) and in
`PATCH /api/config` (`"review": true` toggles it on). Follows the same
`applyBoolToggle` wiring in `config.go:344`.

### tryAutoAdon (`internal/handler/tasks_autopilot.go`)

New function called from the autopilot tick, parallel to `tryAutoTest`:

```go
func (h *Handler) tryAutoAdon(ctx context.Context) {
    if !h.ReviewEnabled() {
        return
    }
    tasks := h.store.ListWaitingTasksWithSession(ctx)  // filter: SessionID != nil, ReviewUnresolved == nil
    for _, t := range tasks {
        go h.runReview(ctx, t)
    }
}
```

`runReview`:
1. Computes the diff (reusing the worktree diff path already used by `tryAutoTest`).
2. Builds `adversarial.Input` from task fields:
   - `TaskPrompt = t.Prompt`
   - `Criteria = t.Criteria` (from test-criteria spec; empty string if not set)
   - `SessionID = *t.SessionID`
   - `DiffPatch = diff.Patch`
   - `Cwd = worktreePath`
   - `StateDir = worktreePath + "/.review"`
   - `ForkCount = 2` (default, configurable later)
   - `MaxRounds = 4`
   - `CostCapTokens = 50000`
3. Calls `h.verifier.Verify(ctx, input)`.
4. Persists the result to `Task.ReviewUnresolved`, `ReviewHeadline`, `ReviewSessionDir`
   via a store update.
5. On error: logs, leaves `ReviewUnresolved == nil` so the task can be retried next
   tick.

`ListWaitingTasksWithSession` is a new store query: tasks in `waiting` status where
`session_id IS NOT NULL AND review_unresolved IS NULL`. This avoids re-running review
on tasks that were already verified.

The verifier instance is created in `Handler.init` and stored on `Handler`:
```go
h.verifier = adversarial.NewReviewVerifier(h.runner, h.harness)  // when ReviewEnabled
// swapped to NoopVerifier when disabled
```

Alternatively, `ReviewVerifier.Verify` can check `h.ReviewEnabled()` internally, but
keeping the verifier swap at the handler level keeps the `Verifier` interface free
of handler coupling.

### Manual trigger (`internal/handler/execute.go`)

`POST /api/tasks/{id}/review` — available when the task is in `waiting` status and has
a `SessionID`. Returns 202 Accepted; the run happens asynchronously. Response body
includes the `ReviewSessionDir` path so the user can watch `.review/` in real time.

Gate conditions (same shape as `TestTask`):
- Task must exist and belong to the caller.
- Status must be `waiting`.
- `SessionID` must be non-nil.
- No reviewEnabled check — the manual trigger always works regardless of the global
  toggle (skippability is for the *autopilot* path, not the explicit manual call).

### Frontend (`frontend/src/components/TaskDetail.vue`)

**Types** (`frontend/src/api/types.ts`): add to `Task` interface:
```typescript
review_unresolved?: number   // undefined = not run, 0 = clean, >0 = issues found
review_headline?: string
```

**Badge in task header**: when `task.review_unresolved !== undefined`:
- `review_unresolved === 0`: show a small green "Review: clean" indicator.
- `review_unresolved > 0`: show a yellow/orange "Review: N unresolved" indicator.
  Clicking it expands a `<details>` block showing `review_headline` as rendered
  markdown.

**"Review" button** (next to the existing "Test" button): visible when the task is in
`waiting` status and `task.session_id` is present. Clicking sends
`POST /api/tasks/{id}/review`. Same loading/error state pattern as the Test button.

The button is always rendered (not gated on a global config flag) because the manual
trigger has no reviewEnabled requirement — see above.

## Defaults and conservative settings

The autopilot defaults are deliberately conservative to avoid runaway cost during
initial rollout:

| parameter      | default | rationale                                        |
|----------------|---------|--------------------------------------------------|
| ForkCount      | 2       | two independent critics per run                  |
| MaxRounds      | 4       | 4 debate rounds each; caps at 8 critic calls     |
| CostCapTokens  | 50 000  | roughly $0.15 at current Claude Sonnet pricing   |

These are hard-coded defaults in `tryAutoAdon`. Future spec: expose them in
`GET/PATCH /api/config`.

## Relationship to test-criteria

`Task.Criteria` (from [[test-criteria]]) flows into `Input.Criteria`, which
`buildTaskContext` appends to the task context. Critics see "Acceptance Criteria:"
and anchor their attacks to what the user actually wanted verified. The two features
are orthogonal: test-criteria shapes the single-agent test path; review adds an
adversarial multi-agent layer. Either works without the other.

## Non-Goals

- Replacing `tryAutoTest`. Review is additive; the test agent path is unchanged.
- Auto-submit gating on review results. `ReviewUnresolved > 0` does not block
  auto-submit in v1. This is a deliberate choice to avoid blocking completions
  until review's false-positive rate is understood in practice.
- Adding a `SnapshotProposer` for non-Claude harnesses (deferred to a follow-up).
- A `pkg/adversarial/codex` critic in review (not needed — wallfacer uses its own
  `HarnessCritic` for codex).
- Surfacing the full review session directory in the UI (the `ReviewSessionDir` path
  is returned in the manual trigger response; a deeper UI is a follow-up).

## Phasing / Acceptance Criteria

**Phase 1 — package skeleton** (`internal/adversarial/`). Implement `NoopVerifier`
and the `ReviewVerifier` stub against `latere.ai/x/review/pkg/adversarial.Verifier`.
Add the `replace` directive to `go.mod` pointing at the local review path. Compile
against a stub `pkg/adversarial` (empty package with just the interfaces) until review
spec 37 lands. Tests: `NoopVerifier.Verify` returns nil; `ReviewVerifier` with a mock
engine returns the correct `VerifyResult` fields.

**Phase 2 — Task model + store queries**. Add `ReviewUnresolved *int`,
`ReviewHeadline string`, `ReviewSessionDir string` to `models.go`. Add
`ListWaitingTasksWithSession` store query. Add review fields to `UpdateTask`.
Tests: create a task with session ID; `ListWaitingTasksWithSession` returns it;
`UpdateTask` with review result persists all three fields.

**Phase 3 — autopilot integration**. Add `Handler.reviewEnabled`, `ReviewEnabled()`,
`SetReview()`, `tryAutoAdon`. Wire into config API (`/api/config` GET/PATCH). Tests:
`ReviewEnabled()` defaults false; `SetReview(true)` enables; a waiting task with session
ID triggers `runReview` and stores the result; a task without session ID is skipped;
a task with `ReviewUnresolved != nil` is not re-run.

**Phase 4 — manual trigger**. `POST /api/tasks/{id}/review` endpoint. Tests: 404 on
unknown task; 409 on wrong status; 202 triggers runReview asynchronously; the session
dir path is in the 202 response.

**Phase 5 — frontend**. `review_unresolved` / `review_headline` in `Task` type. Badge in
task header. "Review" button on waiting tasks with session ID. Acceptance: a task that
ran through review shows the correct badge; clicking "Review" on a waiting task sends
the POST and the badge updates when the result arrives.
