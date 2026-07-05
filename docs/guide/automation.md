# Automation

Wallfacer operates anywhere on the spectrum from fully manual to fully hands-off. At one end, every card is dragged by hand and every result reviewed before commit. At the other, the backlog is loaded, every toggle is on, and tasks execute, verify, submit, and push without intervention. This page covers the toggles, the background watchers behind them, adversarial verification with Agon, and the safety systems that keep hands-off operation from running away.

## The automation toggles

Five switches drive the pipeline. They live in the board's **Automation** popover (the lightning bolt in the header; a dot marks that at least one switch is on) and, together with the numeric execution knobs, on the Execution settings tab (`/settings?tab=execution`). Both surfaces read and write the same server-side state.

| Label | Config key | What it controls |
|---|---|---|
| Implement | `autoimplement` | Auto-promote backlog tasks into In Progress |
| Test | `autotest` | Run verification on waiting tasks automatically |
| Submit | `autosubmit` | Mark waiting tasks done once verified |
| Catch up | `autosync` | Rebase waiting tasks onto the default branch |
| Push | `autopush` | Push completed commits upstream |

A sixth switch, `agon`, enables adversarial verification (below). It is stored in the same runtime configuration but is not part of the board popover; toggle it through the configuration API (`PUT /api/config` with `{"agon": true}`). A manual per-task **Agon** action is also available on the task detail actions rail.

Each toggle arms one server-side watcher. The watchers wake on store changes and on periodic tickers (30 to 60 seconds), scoped to the currently viewed workspace.

## What each watcher does

### Implement: the auto-promoter

When capacity allows, the auto-promoter moves backlog tasks to In Progress and launches their agents. Eligibility and ordering:

- **Parallel cap**: the global limit is `WALLFACER_MAX_PARALLEL`; a workspace can override it with its own `MaxParallel` (0 means unlimited for that workspace).
- **Dependencies**: a task is promoted only when every task it depends on is done.
- **Scheduled time**: a task with a future `ScheduledAt` waits; a precise one-shot timer promotes it within milliseconds of the due time.
- **Ordering**: candidates are ranked by critical-path score (tasks that unblock the most downstream work go first), then board position, then creation time.
- **Skips**: routine cards (driven by the routine engine, see [Routines](routines.md)) and tasks currently locked by a planning agent are never promoted.

The same watcher also auto-resumes waiting tasks that carry failed-test feedback, feeding the feedback back into the session, up to a cap of 3 consecutive test failures. After the cap, the task parks until manual feedback arrives.

### Auto-retry (always on)

Failed tasks with a transient infrastructure failure category are reset to Backlog for another attempt. This watcher has no toggle; it is bounded by budgets instead:

- **Per-category budgets** per task: `container_crash` 2, `sync_error` 2, `worktree_setup` 1.
- **Global cap**: at most 3 auto-retries per task across all categories.
- Container-crash retries are suppressed while the agent-launch circuit breaker is open.

Agent errors, timeouts, budget overruns, and unknown failures are never auto-retried; they wait for human review. A manual retry restores the full budget.

### Test: the auto-tester

Waiting tasks that have not been verified (`LastTestResult` empty), have all worktrees present, and are not behind the default branch get a test agent run. Test runs have their own concurrency limit (`WALLFACER_MAX_TEST_PARALLEL`), independent of the regular cap. When Agon supersedes testing for a task (below), the auto-tester skips it so the two verifiers never double up.

### Submit: the auto-submitter

Verified waiting tasks move to done automatically. The gate depends on the verifier in play:

- **Test gate** (default): the task's last test result is `pass`. As a shortcut, a task that ended naturally (`end_turn` stop reason) and was never tested qualifies, but only while auto-test is off; with auto-test on, testing runs first.
- **Agon gate**: when Agon supersedes the test agent for a task, the gate is a clean Agon verdict (zero unresolved attacks). The test-pass and natural-completion shortcuts do not apply.

In addition, every worktree must be up to date with the default branch and free of merge conflicts. Tasks with a session go through the commit pipeline (committing state, commit message generation); sessionless tasks move straight to done. Tasks that failed testing are never auto-submitted.

### Catch up: the waiting-sync watcher

Every 30 seconds, waiting tasks whose worktrees have fallen behind the default branch are rebased onto it, exactly as if **Sync** were clicked. Sync is a lightweight host-side git rebase; it does not launch an agent and bypasses the parallel cap, so waiting tasks stay current even at full capacity. A failed `git fetch` is recorded on the task and the sync is skipped until it clears.

### Push: auto-push

After the commit pipeline completes, each workspace repo whose local branch is at least the threshold number of commits ahead of upstream gets a `git push`. Configure with `WALLFACER_AUTO_PUSH` and `WALLFACER_AUTO_PUSH_THRESHOLD` (default threshold 1), or from the Execution settings tab. Push results land on the task timeline.

## Adversarial verification with Agon

Agon replaces the single test agent with a structured debate about the change. A **proposer** (always Claude, forked from the task's session) defends the work; one or more **critics** (rotating Claude and Codex for perspective diversity) attack it over multiple rounds. Attacks a proposer cannot rebut remain *unresolved*.

Scope and behavior:

- Agon supersedes the test agent **only for tasks with a session** (Agon forks the session to build the proposer). Sessionless tasks fall back to the regular test agent even with Agon on.
- Eligible waiting tasks are verified automatically when the `agon` toggle is on; at most 2 Agon runs execute concurrently, outside the regular task caps.
- A clean verdict (zero unresolved attacks) lets auto-submit proceed. Any unresolved attack is a hard barrier: the task stays parked in waiting and is not auto-resumed. Clearing the barrier is a human act, either confirming the work or resuming with steering, which discards the verdict and triggers fresh re-verification.
- The full debate transcript, verdict, and cost render in the task detail's Adversarial Verification panel; Agon spend is attributed to the task's usage breakdown.

Depth knobs, as environment variables:

| Variable | Default | Meaning |
|---|---|---|
| `WALLFACER_AGON_FORKS` | 1 | Independent critic forks per run |
| `WALLFACER_AGON_ROUNDS` | 3 | Round cap per fork (attack, rebuttal, re-assessment) |
| `WALLFACER_AGON_COST_CAP` | 50000 | Soft token budget per run |

The defaults are a minimum-cost floor, not a recommended depth; fewer than 3 rounds would end a debate before the critic sees the rebuttal.

## Circuit breakers and safety valves

Two independent breaker systems pause automation when something goes wrong, and self-heal without intervention.

### Watcher breakers

Each watcher (auto-promote, auto-retry, auto-test, auto-submit, auto-sync, auto-agon) has its own breaker. Repeated errors in one watcher's scan-and-act cycle open its breaker and suppress that watcher alone; all others keep running. Recovery uses exponential backoff: 30 seconds, doubling per failure, capped at 5 minutes. A single success resets the breaker. Per-watcher health (failure count, retry time, last reason) is reported in the config API response (`watcher_health`). Breakers only suppress automated actions; manual board operations keep working, and the toggles themselves are unaffected.

### Agent-launch breaker

The runner tracks consecutive agent-launch failures. After `WALLFACER_CONTAINER_CB_THRESHOLD` consecutive failures (default 5) the breaker opens for `WALLFACER_CONTAINER_CB_OPEN_SECONDS` (default 30). While open, auto-promotion halts and container-crash auto-retries are suppressed, preventing a runtime outage from cascading across the whole backlog. The state is exported as the `wallfacer_circuit_breaker_open` Prometheus gauge.

### Other safety valves

- **Context exhaustion**: if any task stops with the `max_tokens` reason, the Implement toggle is switched off automatically. Continuing blindly would burn budget without progress; re-enable after addressing the oversized task.
- **Test-fail cap**: auto-resume from failed-test feedback stops after 3 consecutive failures per task.
- **Turn output truncation**: per-turn agent output is capped by `WALLFACER_MAX_TURN_OUTPUT_BYTES` (default 8 MB); truncated turns are marked on the task record.

## Failure categories and triage

Every failed task carries a failure category, visible on the card and used to decide retry policy:

| Category | Meaning | Auto-retried |
|---|---|---|
| `container_crash` | Agent process died unexpectedly | Yes (budget 2) |
| `sync_error` | Rebase or merge failure during sync | Yes (budget 2) |
| `worktree_setup` | Worktree creation failed | Yes (budget 1) |
| `timeout` | Task exceeded its time limit | No |
| `budget_exceeded` | Token or cost budget exhausted | No |
| `agent_error` | The agent itself reported failure | No |
| `unknown` | Unclassified | No |

Triage guidance: transient categories usually clear themselves via auto-retry; recurring `container_crash` suggests a runtime or credential problem (check `wallfacer doctor`); `agent_error` and `timeout` mean the task needs a better prompt, smaller scope, or manual feedback. Failed-task counts per category are exported on `/metrics` as `wallfacer_failed_tasks_by_category`.

## Related pages

- [Board](board.md) for the task lifecycle these watchers drive.
- [Agent Graph](agent-graph.md) for the fleets tasks execute against.
- [Routines](routines.md) for scheduled task creation, which automation deliberately ignores.
- [Plan](plan.md) for dispatching specs into the board tasks automation picks up.
- [Oversight](oversight.md) for timelines, verdicts, and cost attribution.
- [Mission Control](mission-control.md) for a pipeline-wide view of specs and tasks in flight.
- [Configuration](configuration.md) for the full environment variable reference.
- [Internals: automation](../internals/automation.md) for watcher design and the two-phase protocol.
