---
title: Agent Resource Governance (Priority + Global Budget + Tunable Verification Depth)
status: complete
depends_on: []
affects:
  - internal/executor/host.go
  - internal/executor/spec.go
  - internal/handler/tasks_autoimplement.go
  - internal/handler/config.go
  - internal/constants/constants.go
  - internal/envconfig/envconfig.go
  - frontend/src/components/settings/SettingsTabExecution.vue
  - frontend/src/api/types.ts
effort: xlarge
created: 2026-06-28
updated: 2026-06-30
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
   `agonForkCount = 2`, `agonMaxRounds = 4` (`tasks_autoimplement.go:1199-1203`); a
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

The CPU sink is not the agent CLI itself — it is the **build/test/vite/ripgrep
subprocesses the agent spawns**. (Hitting *Test*, which is roughly one agent, pegs
the machine; so count is not the driver — the tool subtree is.) The fix must
therefore throttle the whole descendant tree, not just the leader. This is the
load-bearing workstream; C and B do not touch the tool children.

In the host launch path (`launchPlainHostAgent` / `launchClaude`), before
`cmd.Start()`:

```go
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // own process group
```

After a successful `Start()`, call best-effort `applyAgentPriority(pid)`, split
**darwin / linux / windows** (not unix / windows — macOS needs its own lever):

- `host_priority_darwin.go`: macOS `nice` is a weak signal. Use the XNU
  background-task policy instead:
  `unix.Setpriority(PRIO_DARWIN_PROCESS, pid, PRIO_DARWIN_BG)` — the syscall
  behind `taskpolicy -b`. It throttles CPU **and** I/O and steers the process to
  efficiency cores, and the background state is **inherited by descendants**
  spawned afterward (so the agent's build/test children are throttled too).
  `golang.org/x/sys/unix` v0.43.0 does **not** export these constants, so define
  them locally: `PRIO_DARWIN_PROCESS = 4`, `PRIO_DARWIN_BG = 0x1000`. Verify a
  parent can set BG on a child pid on the target macOS (some XNU versions only
  allow self-backgrounding — OQ-3); on `EPERM`, fall back to plain
  `Setpriority(PRIO_PGRP, pgid, nice)`.
- `host_priority_linux.go`: `unix.Setpriority(unix.PRIO_PGRP, pgid, nice)` —
  niceness on the group covers the leader and its forked tool children.
- `host_priority_windows.go`: no-op (host mode is `!windows` in tests today).

Promote `golang.org/x/sys` to a direct dependency. A `Setpriority` failure logs
at debug and never fails the launch.

**Teardown must kill the group, not the leader.** Adding `Setpgid` changes kill
semantics: today `signalAndEscalate` signals only `cmd.Process` (the leader), so
once the agent is its own group leader the build/test children would orphan
instead of being reaped. Update teardown to signal the process group
(`syscall.Kill(-pgid, SIGTERM)` then `-pgid, SIGKILL`) on `!windows`; this is
strictly better cleanup. This coupling is mandatory in the same phase as
`Setpgid`, not a follow-up.

Config: `WALLFACER_AGENT_NICE`, default `10`, clamped to `[0, 19]` (0 = no
change); used by the linux path and the darwin fallback. (The darwin BG policy is
a boolean, not a level — the nice value tunes only the non-darwin path.) Read live
per launch (cheap); no restart required.

### B. Global agent budget (`internal/executor` + autoimplement gates)

The executor is the only chokepoint every agent CLI passes through (`b.procs`
already tracks live processes), and crucially there is **no nesting** — the
executor only ever launches top-level agent CLIs; a running agent's tool calls are
its own children, not executor launches. That makes a counting semaphore safe from
self-deadlock.

- Add a buffered acquire/release around `cmd.Start()` / process-exit in
  `HostBackend`, capacity = the global budget. `Launch` acquires (honoring
  `ctx`); the handle's wait/cleanup releases exactly once.
- Keep the per-kind caps (`maxConcurrentTasks` etc.) as cheap *admission* checks
  so the autoimplement does not even begin a unit of work whose silo is full; the
  executor semaphore is the *hard* ceiling on concurrent processes.
- Config: `WALLFACER_MAX_AGENTS`, default a conservative small value (e.g.
  `max(2, NumCPU/2)` — finalize in OQ-1). `0` = unlimited (matches the group
  `0`-means-unlimited sentinel convention).

Lock-ordering note: self-nesting is safe (no held slot launches another agent —
agon critic one-shots are launched by the agon run goroutine, which holds no
executor slot). The real hazard is **stalling the autoimplement**: a blocking
semaphore acquire must never happen under `promoteMu`, a store lock, or inside a
synchronous watcher `Action`. Phase 4 acquires *outside* any lock and launches in
a goroutine, so a full budget delays a launch without serializing the gates.

### C. Minimal agon default (`tasks_autoimplement.go`, `constants.go`)

- `agonForkCount` default `2 → 1`. One fork = one actor/critic pair, Claude-only
  (fork 2, the Codex critic, is opt-in). This halves run count and drops the
  second agent family from the default path.
- `agonMaxRounds` default `4 → 3` — the meaningful floor (OQ-2, resolved). Agon
  alternates roles by round parity: odd = critic, even = proposer. So 3 rounds is
  one full cycle — critic attacks (R1, declares topic), proposer rebuts (R2),
  critic re-assesses and can resolve/withdraw (R3). With only 2 rounds the critic
  never sees the rebuttal, so a valid fix still ends "unresolved" and the hard
  barrier ([[agon-supersede-test]]) would block spuriously. 3 is the floor;
  deeper debate is opt-in.
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

## Outcome (2026-06-28)

All four phases shipped (directly implemented, not dispatched).

- **Phase 1** — agon defaults dropped to the floor (1 fork / 3 rounds);
  `TestAgonTuning_MinimalDefaultsAndOverride` guards the floor + the dial.
- **Phase 2** (load-bearing) — `HostBackend` launches every agent in its own
  process group (`Setpgid`) and throttles it: macOS `PRIO_DARWIN_BG` (CPU+I/O,
  inherited by tool children), Linux `PRIO_PGRP` nice, no-op elsewhere; teardown
  and ctx-cancel kill the group, not just the leader. Split across
  `host_proc_{unix,windows}.go` and `host_priority_{darwin,linux,other}.go`.
  Throttle on by default (nice 10). Test proves group-kill reaps children and
  the nice lands on the leader.
- **Phase 4** — global budget: a counting semaphore in `HostBackend`
  (`WALLFACER_MAX_AGENTS`), acquired in `Launch` outside any handler lock, freed
  on `Wait`. Default 0 = unlimited (OQ-1 resolved: opt-in, no silent throughput
  cap). Test covers cap / release / double-release / unlimited.
- **Phase 3** — all knobs (agon forks/rounds/cost-cap, `agent_nice`,
  `max_agents`) surfaced through GET/PUT `/api/env` and the Execution settings
  tab. Agon dials apply live (`agonTuning` re-reads); `agent_nice` / `max_agents`
  apply on restart (read at backend construction), labelled as such in the UI.
  Backend round-trip test + frontend `vue-tsc` green.

`go build ./...`, the executor/handler/envconfig suites, and golangci-lint
(darwin/linux/windows variants) pass. **OQ-3 is the one open empirical check**:
whether `PRIO_DARWIN_BG` on a child pid is permitted on `Darwin 25.x` or hits
`EPERM` (auto-falls back to nice). Confirm on the reporter's machine; if it
`EPERM`s, add a self-backgrounding harness flag.

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

- **OQ-1 — global budget default. RESOLVED: unlimited (opt-in).** Shipped with
  default 0 = unlimited rather than a CPU-derived cap, so behavior is unchanged
  until a user sets a budget. The Phase 2 throttle already addresses the
  responsiveness symptom; a default cap would silently change throughput. Users
  dial a budget in settings when they want a hard ceiling.
- **OQ-2 — agon rounds floor. RESOLVED: 3.** Agon alternates by round parity
  (odd = critic, even = proposer rebuttal), so 3 rounds is the minimum full cycle
  (attack → rebut → re-assess). 2 would never let the critic see the rebuttal, so
  a fixed attack still ends "unresolved" and the hard barrier
  ([[agon-supersede-test]]) — which now *parks the task for human review* rather
  than auto-retrying — would block spuriously. Phase 1 uses `rounds = 3`.
- **OQ-3 — darwin child backgrounding.** Can the parent set `PRIO_DARWIN_BG` on a
  child pid on the target macOS, or only on itself? If only self, the darwin path
  must have the child background itself at startup (a harness flag/env) rather than
  the parent doing it post-`Start`. Verify on `Darwin 25.x` before Phase 2; the
  `EPERM` → nice fallback keeps it safe meanwhile.
- **OQ-4 — priority scope.** Apply the throttle to *all* host agents (simplest;
  the server is the only foreground) or only to verification (test/agon)
  processes? Default to all; revisit if interactive task runs feel starved.
