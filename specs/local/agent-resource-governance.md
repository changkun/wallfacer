---
title: Agent Resource Governance (Priority + Global Budget + Tunable Verification Depth)
status: drafted
depends_on: []
affects:
  - internal/executor/host.go
  - internal/executor/spec.go
  - internal/handler/tasks_autopilot.go
  - internal/handler/config.go
  - internal/constants/constants.go
  - internal/envconfig/envconfig.go
  - frontend/src/components/settings/SettingsTabExecution.vue
  - frontend/src/api/types.ts
effort: xlarge
created: 2026-06-28
updated: 2026-06-28
author: changkun
dispatched_task_id: null
---

# Agent Resource Governance

## Problem

Starting a single task on the board is comfortable, but the moment a user hits
**Test** or **Agon** the whole machine becomes unresponsive. Diagnosis (confirmed
by the reporter: Activity Monitor shows many `claude`/`codex` processes, and the
slowdown persists regardless of which UI tab is open, so it is not the frontend):

1. **No resource ceiling on host agent processes.** `HostBackend` runs each agent
   CLI directly with `os/exec` and explicitly ignores `spec.CPUs` / `spec.Memory`
   (`internal/executor/host.go:72-74`). Each agent runs with
   `--dangerously-skip-permissions` and spawns its own heavy tool subprocesses
   (builds, test runners, ripgrep). Nothing yields CPU to the foreground, so the
   user's editor/browser starve.

2. **Concurrency caps are siloed, not global.** `maxConcurrentTasks` (default 5),
   `maxTestConcurrentTasks` (default 2), and `maxConcurrentAgon` (default 2) are
   independent admission gates. Their worst case sums — there is no envelope on
   the *total* number of live agent CLIs, so Test/Agon stack on top of running
   tasks.

3. **Verification depth is high by default and hidden.** Agon defaults to
   `agonForkCount = 2`, `agonMaxRounds = 4` (`tasks_autopilot.go:1199-1203`); a
   2-fork run also pulls in a second agent family (the Codex critic, fork 2 of the
   `{Claude, Codex}` rotation). These are env-only (`WALLFACER_AGON_*`) with no
   settings UI, so the expensive default is invisible and effectively unchangeable
   for most users.

## Decision

Govern agent execution along three axes, and surface the knobs in settings with a
**minimum-cost default** that users opt to expand:

1. **Priority (nice).** Launch every host agent process in its own process group
   at a lowered OS scheduling priority so agent CLIs and their tool subprocesses
   yield CPU to the foreground (the wallfacer server and the user's apps). This
   restores interactive responsiveness without changing what work runs.
2. **Global budget.** Add a single ceiling on the number of concurrently running
   agent processes across regular tasks, test runs, and agon, enforced at the
   executor so it holds no matter which caller launches.
3. **Tunable, minimal-by-default verification.** Default agon to the floor — **1
   fork, 1 critic** (one actor/critic pair, Claude-only) — and expose forks,
   rounds, cost cap, the nice level, and the global budget in the Execution
   settings tab via the existing `/api/env` path.

## Design

### A. Process priority and grouping (`internal/executor/host.go`)

In the host launch path (`launchPlainHostAgent` / `launchClaude`), before
`cmd.Start()`:

```go
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // own process group
```

After a successful `Start()`, lower the group's priority:

```go
applyAgentPriority(cmd.Process.Pid, nice) // PRIO_PGRP, best-effort
```

`applyAgentPriority` is platform-split to keep `host.go` portable:

- `host_priority_unix.go` (`//go:build !windows`): wraps
  `golang.org/x/sys/unix.Setpriority(unix.PRIO_PGRP, pgid, nice)` (already an
  indirect dependency; promote to direct). Best-effort: a `Setpriority` failure
  logs at debug and does not fail the launch.
- `host_priority_windows.go` (`//go:build windows`): no-op (host mode is
  `!windows` in tests today; keep the build green everywhere).

`Setpgid` is independently useful: it lets a future change signal the whole agent
tool subtree as a group. Niceness set on the leader is inherited by tool children
forked afterward; using `PRIO_PGRP` also covers any already-spawned members.

Config: `WALLFACER_AGENT_NICE`, default `10`, clamped to `[0, 19]` (0 = no
change). Read live per launch (cheap) or via a small cache; no restart required.

### B. Global agent budget (`internal/executor` + autopilot gates)

The executor is the only chokepoint every agent CLI passes through (`b.procs`
already tracks live processes), and crucially there is **no nesting** — the
executor only ever launches top-level agent CLIs; a running agent's tool calls are
its own children, not executor launches. That makes a counting semaphore safe from
self-deadlock.

- Add a buffered acquire/release around `cmd.Start()` / process-exit in
  `HostBackend`, capacity = the global budget. `Launch` acquires (honoring
  `ctx`); the handle's wait/cleanup releases exactly once.
- Keep the per-kind caps (`maxConcurrentTasks` etc.) as cheap *admission* checks
  so the autopilot does not even begin a unit of work whose silo is full; the
  executor semaphore is the *hard* ceiling on concurrent processes.
- Config: `WALLFACER_MAX_AGENTS`, default a conservative small value (e.g.
  `max(2, NumCPU/2)` — finalize in OQ-1). `0` = unlimited (matches the group
  `0`-means-unlimited sentinel convention).

Deadlock note: because no held slot ever launches another agent (agon critic
one-shots are launched by the agon run goroutine, which does not itself hold an
executor slot), the semaphore cannot self-starve. Tests must assert this
invariant (a task turn holding a slot never triggers a nested executor Launch).

### C. Minimal agon default (`tasks_autopilot.go`, `constants.go`)

- `agonForkCount` default `2 → 1`. One fork = one actor/critic pair, Claude-only
  (fork 2, the Codex critic, is opt-in). This halves run count and drops the
  second agent family from the default path.
- `agonMaxRounds` default `4 → 2` (one proposer→critic exchange; deeper debate is
  opt-in). (Confirm the floor that still produces a meaningful verdict — OQ-2.)
- `agonCostCap` unchanged.

These are already env-tunable; this changes the compile-time floor and makes it
the settings default.

### D. Settings surface (`/api/env` + Execution tab)

Reuse the existing env-backed settings path end to end (no new routes):

- **Backend** (`internal/handler/config.go`, `internal/envconfig/envconfig.go`):
  add `agon_forks`, `agon_rounds`, `agon_cost_cap`, `agent_nice`, `max_agents`
  to `envConfigResponse` (GET, with defaults applied) and to the
  `EnvUpdatePayload` + `envconfig.Updates` / `Update()` write path (PUT persists
  to `.env`). Validate bounds server-side (forks ≥ 1, rounds ≥ 1, nice 0–19,
  max_agents ≥ 0). Invalidate the relevant caches on PUT (agon is already
  re-read live; `agent_nice` / `max_agents` need a cache or live read).
- **Frontend** (`SettingsTabExecution.vue`, `api/types.ts`): add numeric inputs
  under a "Verification & Resources" group, defaulting to the minimum, saved via
  the existing `updateEnv()`. Add the fields to `EnvConfig` and
  `EnvUpdatePayload`.

## Phasing / Acceptance Criteria

Independently shippable, ordered by leverage-per-risk. One small commit per phase
(or per sub-step).

**Phase 1 — minimal agon default (C).** Flip `agonForkCount` 2→1 and
`agonMaxRounds` to the confirmed floor. Test: default tuning yields 1 fork / N
rounds; env override still wins. Smallest diff, immediate cost drop.

**Phase 2 — process priority (A).** `Setpgid` + `applyAgentPriority`, platform
files, `WALLFACER_AGENT_NICE`. Test (`!windows`): a launched fake agent runs in
its own process group at the configured niceness (`Getpriority` on the pgid
returns the set value); nice `0` leaves it unchanged; a `Setpriority` error does
not fail the launch.

**Phase 3 — settings exposure (D).** Wire agon params + `agent_nice` through GET/
PUT `/api/env` and the Execution tab. Test: PUT persists to `.env` and GET round-
trips; bounds rejected; a changed value takes effect on the next run without
restart. Frontend: control renders and saves.

**Phase 4 — global budget (B).** Executor semaphore + `WALLFACER_MAX_AGENTS` +
admission integration. Test: with budget N, at most N agent processes run
concurrently across mixed task/test/agon load; release on exit re-opens a slot;
`0` = unlimited; no nested-launch deadlock.

## Non-Goals

- Hard CPU/memory *caps* per agent. macOS has no cgroup-style per-process CPU cap;
  `nice` (reprioritization) is the portable lever and addresses the reported
  symptom (responsiveness). Linux cgroup enforcement can be a later spec.
- Per-workspace-group agon overrides (the group model already carries parallel
  limits; agon-per-group can follow if needed).
- Frontend transcript-render cost. A separate, smaller cleanup: the agon
  verification tab re-parses every round's markdown on each 2.5 s poll
  (`AgonVerification.vue:117` `v-html="renderMarkdown(r.body)"` inside the
  fork/round `v-for`). Real but secondary (tab-only, browser-bound); memoize by
  round body if it becomes a complaint. Tracked here so it is not lost.

## Open Questions

- **OQ-1 — global budget default.** Fixed small constant vs CPU-derived
  (`max(2, NumCPU/2)`)? CPU-derived adapts to the machine but is less
  predictable. Lean CPU-derived; confirm before Phase 4.
- **OQ-2 — agon rounds floor.** Does `agonMaxRounds = 2` still yield a meaningful
  adversarial verdict, or is 3 the practical floor? Validate against agon's round
  semantics before flipping the default in Phase 1.
- **OQ-3 — nice scope.** Apply the lowered priority to *all* host agents
  (simplest; the server is the only foreground) or only to verification (test/
  agon) processes? Default to all; revisit if interactive task runs feel starved.
