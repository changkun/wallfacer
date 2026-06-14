# Circuit Breakers

Wallfacer uses circuit breakers to automatically pause automation when
something goes wrong, preventing cascading failures. They self-heal
once the problem resolves, with no manual intervention required.

There are two independent systems: **watcher breakers** (per-automation)
and the **agent-launch breaker** (runtime-level).

## Watcher Breakers

Each automation watcher (Promote, Retry, Test, Submit, Catch Up) has its
own circuit breaker. When a watcher encounters repeated errors, its
breaker opens and suppresses that watcher while leaving all others
running.

### What you see

The header shows a **Circuit Breakers** section. When all watchers are
healthy, it displays a green dot with "All healthy." When a breaker
trips, a red indicator appears showing:

- The affected watcher name (e.g. "Promote", "Retry")
- How many consecutive failures occurred
- A countdown until the next retry attempt
- The error reason (hover for details)

### What triggers a watcher breaker

A breaker opens when the watcher's internal logic encounters an error
during its scan-and-act cycle. For example:

- The auto-promoter fails to launch an agent process
- The auto-retrier encounters an unexpected store error
- The auto-submitter fails to commit changes

### How it recovers

Watcher breakers use **exponential backoff**:

| Failure # | Cooldown |
|-----------|----------|
| 1st       | 30 seconds |
| 2nd       | 1 minute |
| 3rd       | 2 minutes |
| 4th       | 4 minutes |
| 5th+      | 5 minutes (cap) |

After each cooldown, the watcher tries again. A single success resets
the breaker completely: the failure counter goes back to zero and the
watcher resumes normal operation.

### What still works

Watcher breakers only affect **automated** actions. You can still:

- Manually promote tasks from the board
- Manually trigger refinement or testing
- Use all other UI features normally

Automation toggles (Autopilot, Auto-test, etc.) remain independent of
breaker state. The breaker is a transient safety layer, not a
configuration change.

## Agent-Launch Breaker

The agent-launch breaker protects against the agent CLI being
unavailable. Wallfacer runs each task by launching the selected agent
(claude or codex) as a host process in the task's git worktree. If that
launch fails repeatedly, wallfacer stops trying to start new agent
processes until the problem clears.

### What triggers it

The agent-launch breaker opens after **5 consecutive launch failures**
(configurable). Only failures to *start* the agent process count, for
example:

- The agent binary cannot be found (not on `$PATH`, or the configured
  path does not exist)
- The host process fails to spawn

Normal agent failures (an agent that starts but exits with a non-zero
code) do **not** trip the breaker. Those are task-level outcomes, not
launch failures.

### How it recovers

The agent-launch breaker uses a three-state model:

1. **Closed** (normal): All agent launches proceed.
2. **Open** (tripped): All launches blocked for 30 seconds.
3. **Half-open** (probing): One launch is allowed as a probe.
   - If the probe succeeds, the circuit closes and operations resume.
   - If the probe fails, the circuit reopens for another 30 seconds.

### What you see

The agent-launch breaker is not directly shown in the UI. Its effects
are visible as:

- Auto-promote stops picking up new tasks
- Auto-retry stops retrying tasks that failed to launch
- The board appears to "pause" temporarily

Once the agent CLI can be launched again, the next probe succeeds and
everything resumes automatically.

The breaker state is available via `GET /api/debug/runtime` (the
`container_circuit` field) for monitoring.

## Configuration

Both breakers are configurable via process-level environment variables
(set in your shell or systemd unit):

| Variable | Default | Description |
|----------|---------|-------------|
| `WALLFACER_CONTAINER_CB_THRESHOLD` | 5 | Consecutive agent-process launch failures before the breaker opens |
| `WALLFACER_CONTAINER_CB_OPEN_SECONDS` | 30 | How long the breaker stays open before probing |

Watcher breaker timing (30s base, 5min cap) is not currently
configurable.

## Design Rationale

Circuit breakers exist because wallfacer runs automation continuously in
the background. Without them, a transient failure (e.g. the agent CLI
temporarily missing from `$PATH`) would cause every watcher to spam
failed attempts, flood logs, and potentially exhaust resources. The
breakers provide automatic backoff and recovery with no user action
needed.

Key design choices:

- **Per-watcher isolation**: A failure in auto-test does not affect
  auto-promote. Each watcher recovers independently.
- **User actions bypass breakers**: Manual operations always work,
  even when automation is paused.
- **No manual reset**: Breakers self-heal. The exponential backoff
  ensures recovery within 5 minutes at most. A manual reset could
  cause re-triggering of the same failure.
- **Automatic, not configurable toggles**: Breakers are a safety
  mechanism, not a user preference. They open and close based on
  runtime conditions.
