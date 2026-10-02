# Automation

Wallfacer operates anywhere on the spectrum from fully manual to fully hands-off. At one end, every card is dragged by hand and every result reviewed before commit. At the other, the backlog is loaded, every toggle is on, and tasks execute, verify, submit, and push without intervention. This page covers the toggles, the background watchers behind them, review on a second model, and the safety systems that keep hands-off operation from running away.

## The automation toggles

Five switches drive the pipeline. They live in the board's **Automation** popover (the lightning bolt in the header; a dot marks that at least one switch is on) and, together with the numeric execution knobs, on the Execution settings tab (`/settings?tab=execution`). Both surfaces read and write the same server-side state.

| Label | Config key | What it controls |
|---|---|---|
| Implement | `autoimplement` | Auto-promote backlog tasks into In Progress |
| Test | `autotest` | Run verification on waiting tasks automatically |
| Submit | `autosubmit` | Mark waiting tasks done once verified |
| Catch up | `autosync` | Rebase waiting tasks onto the default branch |
| Push | `autopush` | Push completed commits upstream |

A sixth switch, `review`, enables Review on a second model (below). It is stored in the same runtime configuration but is not part of the board popover; toggle it through the configuration API (`PUT /api/config` with `{"review": true}`). A manual per-task **Review** action is also available on the task detail actions rail.

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

Waiting tasks that have not been verified (`LastTestResult` empty), have all worktrees present, and are not behind the default branch get a test agent run. Test runs have their own concurrency limit (`WALLFACER_MAX_TEST_PARALLEL`), independent of the regular cap. When Review supersedes testing for a task (below), the auto-tester skips it so the two verifiers never double up.

### Submit: the auto-submitter

Verified waiting tasks move to done automatically. The gate depends on the verifier in play:

- **Test gate** (default): the task's last test result is `pass`. As a shortcut, a task that ended naturally (`end_turn` stop reason) and was never tested qualifies, but only while auto-test is off; with auto-test on, testing runs first.
- **Review gate**: when Review supersedes the test agent for a task, the gate is a Review approval (no open findings). The test-pass and natural-completion shortcuts do not apply.

In addition, every worktree must be up to date with the default branch and free of merge conflicts. Tasks with a session go through the commit pipeline (committing state, commit message generation); sessionless tasks move straight to done. Tasks that failed testing are never auto-submitted.

### Catch up: the waiting-sync watcher

Every 30 seconds, waiting tasks whose worktrees have fallen behind the default branch are rebased onto it, exactly as if **Sync** were clicked. Sync is a lightweight host-side git rebase; it does not launch an agent and bypasses the parallel cap, so waiting tasks stay current even at full capacity. A failed `git fetch` is recorded on the task and the sync is skipped until it clears.

### Push: auto-push

After the commit pipeline completes, each workspace repo whose local branch is at least the threshold number of commits ahead of upstream gets a `git push`. Configure with `WALLFACER_AUTO_PUSH` and `WALLFACER_AUTO_PUSH_THRESHOLD` (default threshold 1), or from the Execution settings tab. Push results land on the task timeline.

## Review on a second model

Review checks a waiting task's change with a second model. A **reviewer** runs on the model set as `WALLFACER_REVIEW_MODEL` (the **Review model** field on the Harness settings tab), on the harness the task runs on, and reads the task prompt, the acceptance criteria, and the task's diff against the default branch. It answers with findings, each a severity (high, medium, or low) and a one-sentence claim, and a verdict: approve, or changes requested.

When the reviewer requests changes, its findings go to the task as feedback, the same way feedback typed on a waiting task does, and the task takes another turn. When the task waits again, the reviewer reviews the new diff with its earlier findings and the task's reply in view. A review session ends when:

- the reviewer approves: the task carries no open findings, and auto-submit can proceed;
- the last allowed round still requests changes, or the reviewer token budget is spent: the findings of the last round stay open, and the most severe one becomes the task's review headline.

Open findings at the end of a session are a hard barrier: the task stays parked in waiting and is not auto-resumed. Clearing the barrier is a human act, either confirming the work or resuming with feedback, which discards the verdict and starts a new review session once the task waits again.

Scope and behavior:

- A review needs a second model. When `WALLFACER_REVIEW_MODEL` is unset, or names the model the task runs on, the review does not run: the task timeline says why, once, and the task carries no verdict, so with Review on it is not auto-submitted.
- Review supersedes the test agent for every waiting task that has a worktree, on any harness. A task without a worktree has no diff to review and falls back to the regular test agent; a task whose worktree has gone missing is not reviewed until it is restored.
- Eligible waiting tasks are reviewed automatically when the `review` toggle is on. One round runs at a time per task, and at most 2 rounds run at once across tasks, outside the regular task caps.
- A reviewer run that fails, or answers in a form that cannot be read, does not use up a round: the task timeline says so, and the round is retried once the auto-review breaker's backoff has passed.
- The reviewer reads the diff from its prompt, capped at 16,000 bytes, and runs without access to the workspace, so it cannot change the task's worktree.
- The task detail's Verification tab shows each round's findings, the feedback sent, and the task's reply. Reviewer spend is attributed to the task's usage breakdown under `review`. A task on the in-process Topos harness reports no usage yet, so its review is bounded by the round limit alone.

Settings, as environment variables:

| Variable | Default | Meaning |
|---|---|---|
| `WALLFACER_REVIEW_MODEL` | unset | The reviewer's model; must differ from the task's |
| `WALLFACER_REVIEW_ROUNDS` | 3 | Reviewer runs per review session; the task gets at most one turn fewer on requested changes |
| `WALLFACER_REVIEW_COST_CAP` | 50000 | Reviewer token budget per review session (input plus output tokens), checked between rounds |

## Circuit breakers and safety valves

Two independent breaker systems pause automation when something goes wrong, and self-heal without intervention.

### Watcher breakers

Each watcher (auto-promote, auto-retry, auto-test, auto-submit, auto-sync, auto-review) has its own breaker. Repeated errors in one watcher's scan-and-act cycle open its breaker and suppress that watcher alone; all others keep running. Recovery uses exponential backoff: 30 seconds, doubling per failure, capped at 5 minutes. A single success resets the breaker. Per-watcher health (failure count, retry time, last reason) is reported in the config API response (`watcher_health`). Breakers only suppress automated actions; manual board operations keep working, and the toggles themselves are unaffected.

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
| `model_credential_missing` | A run on the in-process `topos` harness was refused because no model credential is set; nothing ran | No |
| `unknown` | Unclassified | No |

Triage guidance: transient categories usually clear themselves via auto-retry; recurring `container_crash` suggests a runtime or credential problem (check `wallfacer doctor`); `agent_error` and `timeout` mean the task needs a better prompt, smaller scope, or manual feedback. Failed-task counts per category are exported on `/metrics` as `wallfacer_failed_tasks_by_category`.

## Related pages

- [Board](board.md) for the task lifecycle these watchers drive.
- [Routines](routines.md) for scheduled task creation, which automation deliberately ignores.
- [Plan](plan.md) for dispatching specs into the board tasks automation picks up.
- [Oversight](oversight.md) for timelines, verdicts, and cost attribution.
- [Mission Control](mission-control.md) for a pipeline-wide view of specs and tasks in flight.
- [Configuration](configuration.md) for the full environment variable reference.
- [Internals: automation](../internals/automation.md) for watcher design and the two-phase protocol.
