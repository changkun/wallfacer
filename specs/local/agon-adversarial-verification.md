---
title: Agon Adversarial Verification
status: drafted
depends_on: []
affects:
  - internal/adversarial/agon.go
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
updated: 2026-06-26
author: changkun
dispatched_task_id: null
---

# Agon Adversarial Verification

## Problem

After the implementation agent finishes a task, wallfacer's only automated
verification is a single test agent. A single agent cannot find bugs it introduced
itself; it has the same blind spots as the implementation run. Adversarial
multi-agent debate — multiple critic agents attacking the changes, the proposer
rebuttng — is proven to surface issues the implementer missed.

Agon (`latere.ai/x/agon`, at `~/dev/latere.ai/agon`) implements exactly this
protocol. Today it is a standalone CLI. Its core logic is under `internal/` and
cannot be imported by wallfacer or any other Go module. The test step can be skipped
globally; no equivalent mechanism exists for adversarial verification.

## Goal

1. Extract agon's engine, interfaces, and result types into a public
   `pkg/adversarial` API (prerequisite in the agon repo — see
   `~/dev/latere.ai/agon/specs/37-pkg-public-api.md`).
2. Define a `Verifier` interface in `internal/adversarial/` — the wallfacer-internal
   plugin seam — so other verification strategies can plug in alongside the agon
   implementation.
3. Implement `HarnessCritic`: a wallfacer-owned adapter that satisfies
   `adversarial.Critic` using the runner's existing agent-invocation path. This
   makes all five wallfacer harnesses available as critics without importing
   agon's bundled claude/codex drivers.
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

This spec **requires** `latere.ai/x/agon/pkg/adversarial` to be importable first.
That extraction is specified in agon `specs/37-pkg-public-api.md`. While that spec
is being implemented, wallfacer's `go.mod` should add a local `replace` directive:

```
replace latere.ai/x/agon => ../../agon
```

Phase 1 of this spec (the `internal/adversarial/` package) can be drafted and
tested with a stub that returns a fixed `Result{}` until the agon extraction
lands; the stub is replaced in Phase 2.

## Design

### The `Verifier` interface (agon-owned, not wallfacer-owned)

The `adversarial.Verifier` interface and its `VerifyInput`/`VerifyResult` types are
defined in `latere.ai/x/agon/pkg/adversarial` (see agon
`specs/37-pkg-public-api.md`). Wallfacer imports them; it does not redefine them.
This makes `adversarial.Verifier` the canonical, latere-wide integration seam —
any latere tool that wants to embed adversarial verification satisfies the same
interface without per-tool reinvention.

`internal/adversarial/` in wallfacer is therefore a pure implementation package:
it provides concrete types that satisfy `adversarial.Verifier` and `adversarial.Critic`,
wired to wallfacer's runner infrastructure. It defines no new interface types.

**`internal/adversarial/noop.go`**: `NoopVerifier` — satisfies `adversarial.Verifier`,
`Verify` returns `(nil, nil)` immediately. Active when agon is toggled off, zero cost
and zero behavioral change for the skip path.

### HarnessCritic (`internal/adversarial/harness_critic.go`)

`HarnessCritic` satisfies `adversarial.Critic` using wallfacer's existing
`runner.runAgent` infrastructure. Each `Round` call is a one-shot stateless agent
invocation: the critic prompt is assembled from the agon protocol fields
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
OpenCode, Claude, and Pi can all serve as critics without any agon-side driver
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

### AgonVerifier (`internal/adversarial/agon.go`)

`AgonVerifier` is the concrete implementation of `adversarial.Verifier` (the agon
package interface). It wires the proposer and critic into an `adversarial.Engine`
and calls `Run`:

```go
// AgonVerifier implements latere.ai/x/agon/pkg/adversarial.Verifier.
type AgonVerifier struct {
    runner  *runner.Runner
    harness harness.Harness
}

func (v *AgonVerifier) Verify(ctx context.Context, in adversarial.VerifyInput) (*adversarial.VerifyResult, error) {
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

// buildTaskContext merges prompt and criteria into agon's TaskContext field.
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
AgonUnresolved *int   `json:"agon_unresolved,omitempty"` // nil = not yet run
AgonHeadline   string `json:"agon_headline,omitempty"`
AgonSessionDir string `json:"agon_session_dir,omitempty"`
```

- `AgonUnresolved == nil`: agon has not run for this task (or is disabled globally).
- `AgonUnresolved == 0`: agon ran and found no unresolved attacks. Clean.
- `AgonUnresolved > 0`: agon found open disputes; `AgonHeadline` holds the summary.

No migration needed: `omitempty` on `*int` means nil serializes to absent; existing
task JSON files deserialize with `AgonUnresolved == nil`.

Store updates:
- `UpdateTask` (handler PATCH at `tasks_update.go`) — add setter for the three agon
  fields; only the autopilot and the manual trigger write these, not user-initiated
  PATCH.

### Handler toggle (`internal/handler/handler.go`)

Add an `agonEnabled atomic.Bool` field to `Handler`, following the `autotest` pattern
exactly:

```go
agonEnabled atomic.Bool   // in Handler struct, near autotest
```

```go
func (h *Handler) AgonEnabled() bool     { return h.agonEnabled.Load() }
func (h *Handler) SetAgon(enabled bool)  { h.agonEnabled.Store(enabled) }
```

Default: false. Surfaces in `GET /api/config` response (`"agon": false`) and in
`PATCH /api/config` (`"agon": true` toggles it on). Follows the same
`applyBoolToggle` wiring in `config.go:344`.

### tryAutoAdon (`internal/handler/tasks_autopilot.go`)

New function called from the autopilot tick, parallel to `tryAutoTest`:

```go
func (h *Handler) tryAutoAdon(ctx context.Context) {
    if !h.AgonEnabled() {
        return
    }
    tasks := h.store.ListWaitingTasksWithSession(ctx)  // filter: SessionID != nil, AgonUnresolved == nil
    for _, t := range tasks {
        go h.runAgon(ctx, t)
    }
}
```

`runAgon`:
1. Computes the diff (reusing the worktree diff path already used by `tryAutoTest`).
2. Builds `adversarial.Input` from task fields:
   - `TaskPrompt = t.Prompt`
   - `Criteria = t.Criteria` (from test-criteria spec; empty string if not set)
   - `SessionID = *t.SessionID`
   - `DiffPatch = diff.Patch`
   - `Cwd = worktreePath`
   - `StateDir = worktreePath + "/.agon"`
   - `ForkCount = 2` (default, configurable later)
   - `MaxRounds = 4`
   - `CostCapTokens = 50000`
3. Calls `h.verifier.Verify(ctx, input)`.
4. Persists the result to `Task.AgonUnresolved`, `AgonHeadline`, `AgonSessionDir`
   via a store update.
5. On error: logs, leaves `AgonUnresolved == nil` so the task can be retried next
   tick.

`ListWaitingTasksWithSession` is a new store query: tasks in `waiting` status where
`session_id IS NOT NULL AND agon_unresolved IS NULL`. This avoids re-running agon
on tasks that were already verified.

The verifier instance is created in `Handler.init` and stored on `Handler`:
```go
h.verifier = adversarial.NewAgonVerifier(h.runner, h.harness)  // when AgonEnabled
// swapped to NoopVerifier when disabled
```

Alternatively, `AgonVerifier.Verify` can check `h.AgonEnabled()` internally, but
keeping the verifier swap at the handler level keeps the `Verifier` interface free
of handler coupling.

### Manual trigger (`internal/handler/execute.go`)

`POST /api/tasks/{id}/agon` — available when the task is in `waiting` status and has
a `SessionID`. Returns 202 Accepted; the run happens asynchronously. Response body
includes the `AgonSessionDir` path so the user can watch `.agon/` in real time.

Gate conditions (same shape as `TestTask`):
- Task must exist and belong to the caller.
- Status must be `waiting`.
- `SessionID` must be non-nil.
- No agonEnabled check — the manual trigger always works regardless of the global
  toggle (skippability is for the *autopilot* path, not the explicit manual call).

### Frontend (`frontend/src/components/TaskDetail.vue`)

**Types** (`frontend/src/api/types.ts`): add to `Task` interface:
```typescript
agon_unresolved?: number   // undefined = not run, 0 = clean, >0 = issues found
agon_headline?: string
```

**Badge in task header**: when `task.agon_unresolved !== undefined`:
- `agon_unresolved === 0`: show a small green "Agon: clean" indicator.
- `agon_unresolved > 0`: show a yellow/orange "Agon: N unresolved" indicator.
  Clicking it expands a `<details>` block showing `agon_headline` as rendered
  markdown.

**"Agon" button** (next to the existing "Test" button): visible when the task is in
`waiting` status and `task.session_id` is present. Clicking sends
`POST /api/tasks/{id}/agon`. Same loading/error state pattern as the Test button.

The button is always rendered (not gated on a global config flag) because the manual
trigger has no agonEnabled requirement — see above.

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
are orthogonal: test-criteria shapes the single-agent test path; agon adds an
adversarial multi-agent layer. Either works without the other.

## Non-Goals

- Replacing `tryAutoTest`. Agon is additive; the test agent path is unchanged.
- Auto-submit gating on agon results. `AgonUnresolved > 0` does not block
  auto-submit in v1. This is a deliberate choice to avoid blocking completions
  until agon's false-positive rate is understood in practice.
- Adding a `SnapshotProposer` for non-Claude harnesses (deferred to a follow-up).
- A `pkg/adversarial/codex` critic in agon (not needed — wallfacer uses its own
  `HarnessCritic` for codex).
- Surfacing the full agon session directory in the UI (the `AgonSessionDir` path
  is returned in the manual trigger response; a deeper UI is a follow-up).

## Phasing / Acceptance Criteria

**Phase 1 — package skeleton** (`internal/adversarial/`). Implement `NoopVerifier`
and the `AgonVerifier` stub against `latere.ai/x/agon/pkg/adversarial.Verifier`.
Add the `replace` directive to `go.mod` pointing at the local agon path. Compile
against a stub `pkg/adversarial` (empty package with just the interfaces) until agon
spec 37 lands. Tests: `NoopVerifier.Verify` returns nil; `AgonVerifier` with a mock
engine returns the correct `VerifyResult` fields.

**Phase 2 — Task model + store queries**. Add `AgonUnresolved *int`,
`AgonHeadline string`, `AgonSessionDir string` to `models.go`. Add
`ListWaitingTasksWithSession` store query. Add agon fields to `UpdateTask`.
Tests: create a task with session ID; `ListWaitingTasksWithSession` returns it;
`UpdateTask` with agon result persists all three fields.

**Phase 3 — autopilot integration**. Add `Handler.agonEnabled`, `AgonEnabled()`,
`SetAgon()`, `tryAutoAdon`. Wire into config API (`/api/config` GET/PATCH). Tests:
`AgonEnabled()` defaults false; `SetAgon(true)` enables; a waiting task with session
ID triggers `runAgon` and stores the result; a task without session ID is skipped;
a task with `AgonUnresolved != nil` is not re-run.

**Phase 4 — manual trigger**. `POST /api/tasks/{id}/agon` endpoint. Tests: 404 on
unknown task; 409 on wrong status; 202 triggers runAgon asynchronously; the session
dir path is in the 202 response.

**Phase 5 — frontend**. `agon_unresolved` / `agon_headline` in `Task` type. Badge in
task header. "Agon" button on waiting tasks with session ID. Acceptance: a task that
ran through agon shows the correct badge; clicking "Agon" on a waiting task sends
the POST and the badge updates when the result arrives.
