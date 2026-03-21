# Circuit Breakers

Wallfacer uses circuit breakers to automatically pause automation when
something goes wrong, preventing cascading failures. They self-heal
once the problem resolves — no manual intervention required.

There are two independent systems: **watcher breakers** (per-automation)
and the **container breaker** (runtime-level).

## Watcher Breakers

Each automation watcher (Promote, Retry, Test, Submit, Tip-sync,
Refine) has its own circuit breaker. When a watcher encounters repeated
errors, its breaker opens and suppresses that watcher while leaving all
others running.

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

- The auto-promoter fails to launch a container
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
the breaker completely — the failure counter goes back to zero and the
watcher resumes normal operation.

### What still works

Watcher breakers only affect **automated** actions. You can still:

- Manually promote tasks from the board
- Manually trigger refinement or testing
- Use all other UI features normally

Automation toggles (Autopilot, Auto-test, etc.) remain independent of
breaker state. The breaker is a transient safety layer, not a
configuration change.

## Container Breaker

The container breaker protects against the container runtime (Docker or
Podman) being unavailable. If the runtime crashes, gets uninstalled, or
the daemon stops, wallfacer stops trying to launch containers until the
runtime recovers.

### What triggers it

The container breaker opens after **5 consecutive runtime errors**
(configurable). Only true runtime failures count:

- Exit code 125 (container engine failure)
- Connection refused (daemon not running)
- Binary not found

Normal agent failures (exit codes 1–124) do **not** trip the breaker.

### How it recovers

The container breaker uses a three-state model:

1. **Closed** (normal): All container launches proceed.
2. **Open** (tripped): All launches blocked for 30 seconds.
3. **Half-open** (probing): One launch is allowed as a probe.
   - If the probe succeeds → circuit closes, operations resume.
   - If the probe fails → circuit reopens for another 30 seconds.

### What you see

The container breaker is not directly shown in the UI. Its effects are
visible as:

- Auto-promote stops picking up new tasks
- Auto-retry stops retrying tasks that failed due to container crashes
- The board appears to "pause" temporarily

Once the container runtime is available again, the next probe succeeds
and everything resumes automatically.

The container breaker state is available via `GET /api/debug/runtime`
(the `container_circuit` field) for monitoring.

## Configuration

Both breakers are configurable via process-level environment variables
(set in your shell, systemd unit, or container environment):

| Variable | Default | Description |
|----------|---------|-------------|
| `WALLFACER_CONTAINER_CB_THRESHOLD` | 5 | Consecutive runtime failures before the container breaker opens |
| `WALLFACER_CONTAINER_CB_OPEN_SECONDS` | 30 | How long the container breaker stays open before probing |

Watcher breaker timing (30s base, 5min cap) is not currently
configurable.

## Design Rationale

Circuit breakers exist because wallfacer runs automation continuously in
the background. Without them, a transient failure (e.g. Docker daemon
restarting) would cause every watcher to spam failed attempts, flood
logs, and potentially exhaust resources. The breakers provide automatic
backoff and recovery with no user action needed.

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
