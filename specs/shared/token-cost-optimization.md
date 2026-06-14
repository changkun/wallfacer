---
title: Token & Cost Optimization
status: drafted
depends_on: []
affects: [internal/runner/, internal/store/, internal/executor/, internal/envconfig/, internal/handler/, internal/cli/, internal/metrics/, internal/prompts/, frontend/src/components/analytics/]
effort: xlarge
created: 2026-04-01
updated: 2026-06-14
author: changkun
dispatched_task_id: null
---

# Token & Cost Optimization

## Problem

Wallfacer launches Claude Code (and Codex) as host processes in per-task git worktrees, accumulating significant token costs across tasks. Two orthogonal problems exist:

1. **Cache correctness under wallfacer's execution model.** Claude Code's prompt caching system relies on a static/dynamic boundary in system prompts. Wallfacer's per-turn invocation model (each turn is a fresh `claude`/`codex` exec in the worktree) especially `--resume` for feedback loops and multi-turn continuations, may not be exploiting caching optimally or may be triggering cache invalidation unknowingly. There is no visibility into whether cache hits are actually occurring.

2. **Infrastructure-level token reduction.** Even with perfect caching, the raw volume of tokens flowing through tool outputs (git status, test results, file reads) is unnecessarily large. Tools like [RTK](https://github.com/rtk-ai/rtk) demonstrate 60-90% reduction on common dev commands by compressing shell output before the agent sees it.

### Context: How Claude Code Caching Works

From [claude-design-docs](https://github.com/changkun/claude-design-docs):

- **Static/dynamic boundary**: System prompt splits at a sentinel. Static content (tool schemas, safety rules, coding instructions) gets `cache_control: { type: 'ephemeral', scope: 'global' }`. Dynamic content (memory, env info, CLAUDE.md, MCP instructions) follows without cache scope.
- **Cache pricing**: Cache reads cost 10% of full input price ($0.30/MTok vs $3/MTok on Sonnet). First call writes at 1.25x; subsequent calls read at 0.1x. After two calls, net positive.
- **Cache invalidation triggers**: Changes to system prompt bytes, tool schemas, model string, beta headers, CLAUDE.md content mid-session, MCP connector reconnection.
- **Compaction**: When context hits ~(window - 13K), auto-compact summarizes older messages. "From" direction preserves cache prefix; "up_to" direction invalidates it. Post-compaction restoration re-injects ~5-50K tokens of recently-accessed files and skills.
- **Fork agent sharing**: Sub-agents share the parent's cache via byte-identical system prompts and tool pools. Only the final directive diverges.

### Prior Art: cc-budget

[cc-budget](https://github.com/boyand/cc-budget) is a budget awareness tool for Claude Code that uses two extension points:

1. **Status line command** (`statusLine` in settings.json): Claude Code pipes a JSON payload containing `rate_limits` (5-hour and 7-day rolling windows with `used_percentage` and `resets_at`) and `cost` (`total_cost_usd`) on every assistant message. The script formats a rich ANSI status line with a progress bar and pacing marker.

2. **UserPromptSubmit hook**: Fires before each user prompt. Reads persisted state, snapshots usage keyed by `session_id`, checks threshold crossings (90%/95% for 5h, 80%/90% for 7d), and injects a terse warning into `additionalContext` when a threshold is crossed (the agent sees this as part of its prompt context).

Key ideas applicable to wallfacer:

- **Pacing / burn-rate**: A `|` marker on the progress bar shows where usage *should* be for even distribution across the window (`elapsed / window_duration * 100`). Wallfacer can adapt this per-task against `MaxCostUSD`.
- **Per-turn deltas**: Shows `(+N.N%)` or `(+$X.XX)` after each prompt by diffing pre-prompt snapshot vs post-response reading.
- **Threshold warnings injected into agent context**: Under-20-token budget warnings injected via hook output, once per threshold crossing (not spammy). This is the mechanism for Part 4.2's live budget tracking.
- **Peak/off-peak detection**: Uses `Intl.DateTimeFormat` for Anthropic's peak timezone (America/Los_Angeles, 5-11 AM PT weekdays). No external API calls.
- **Daily/monthly cost ledger**: Persisted per-session cost tracking aggregated by day and month, with automatic pruning (48h for snapshots, 31d for ledger).
- **Enterprise discount multiplier**: Config option for negotiated pricing (e.g., `enterprise_discount: 20` means 20% off list).
- **Atomic state persistence**: PID-based temp file + `rename()` for concurrent-safe writes (wallfacer already uses similar patterns in its store).

### Known Bugs (as of early 2026)

Two cache bugs have been [widely reported](https://www.theregister.com/2026/03/31/anthropic_claude_code_limits/):

1. **Sentinel replacement bug**: The standalone Bun binary does a string replacement on a billing sentinel (`cch=24b72`). It replaces the *first* occurrence in serialized JSON. If conversation history contains this string, the wrong instance is replaced, breaking cache prefix on every request. Workaround: install `npx @anthropic-ai/claude-code` on the host instead of the standalone binary.

2. **MCP tool schema injection**: Cloud MCP connectors inject complete tool schemas into every API call regardless of usage. Ahrefs alone adds 100+ tool definitions (tens of thousands of tokens per message). Workaround: disconnect unused connectors.

Additionally, `--resume` has been reported to always break cache in some versions, though this may be version-specific.

### What Wallfacer Controls vs What It Doesn't

Wallfacer runs the agent CLI directly on the host via `HostBackend` (`internal/executor/host.go`), resolving `claude`/`codex` from `$PATH`, in a per-task git worktree as the working directory. There is no sandbox image; everything below is a host-process / host-PATH concern.

| Layer | Wallfacer controls | Claude Code controls |
|-------|-------------------|---------------------|
| Agent launch flags | `--resume`, `--model`, `-p` | N/A |
| Worktree content | AGENTS.md, board.json, worktrees | N/A |
| Shell output volume | Can intercept via hooks/host wrappers | Built-in tools bypass Bash |
| System prompt composition | N/A | Static/dynamic boundary |
| Cache control headers | N/A | `cache_control` blocks |
| Context compaction | N/A | Auto-compact triggers |
| Session state | Session ID persistence | Internal message history |
| MCP tools | Host MCP config (`~/.claude`) | Tool schema serialization |
| Binary variant | Host install (npx vs standalone) | N/A |

---

## Design Goals

1. **Observability**: Know exactly what cache behavior each task turn is getting (cache hit rate, invalidation causes, cost breakdown by cached vs uncached tokens).
2. **Cache correctness**: Ensure wallfacer's execution model (per-turn exec, `--resume`, feedback loops, AGENTS.md delivery) does not unnecessarily invalidate caches.
3. **Token reduction**: Reduce raw token volume flowing through agent turns via output compression, without changing agent behavior.
4. **Regression modeling**: Build a predictive model of expected token consumption per turn to detect anomalous cost spikes early.
5. **Budget intelligence**: Move from post-hoc budget checks to prospective cost estimation with in-context warnings.
6. **Burn-rate pacing**: Per-task pacing indicators showing whether cost consumption is on track relative to budget and progress.
7. **Aggregate cost tracking**: Daily and monthly cost ledgers across all tasks with trend analysis and peak/off-peak awareness.

### Already Shipped (baseline cost visibility)

These pieces exist today and are NOT in scope for remaining work; the rest of this spec builds on them:

- **Per-turn and per-task usage recording**: `store.TaskUsage` and `store.TurnUsageRecord` (`internal/store/models.go`) capture `input_tokens`, `output_tokens`, `cache_read_input_tokens`, `cache_creation_tokens`, and `cost_usd`. Turn records persist to `turn-usage.jsonl` via `internal/store/turn_usage.go`; planning usage persists via `internal/store/planning_usage.go`.
- **Cost/usage API**: `/api/usage` (`internal/handler/usage.go`) and `/api/stats` (`internal/handler/stats.go`) aggregate by status, sub-agent/activity, and day.
- **Cost/usage UI tiles**: `frontend/src/components/analytics/{AnalyticsTabCost.vue, AnalyticsTabUsage.vue, AnalyticsTabTiming.vue}` render total cost, cache-token totals, by-status and by-activity breakdowns, and a daily cost chart.
- **Reactive budget enforcement**: `MaxCostUSD` / `MaxInputTokens` on the task model (`internal/store/models.go`) are checked post-turn in `internal/runner/execute.go` and classified as a budget failure.
- **board.json compaction**: `internal/runner/board.go` already limits sibling task text fields and warns when the manifest grows large (`logBoardManifestSizeWarning`).

The remaining work centers on **cache telemetry** (derived metrics, break detection), **token reduction** (host-side RTK, conditional board generation), **regression / anomaly modeling**, **prospective and live budget intelligence** (warnings, pacing), and the **aggregate ledger**.

---

## Part 1: Cache Observability & Correctness

### 1.1 Cache Telemetry

**Status**: Raw cache-token recording is shipped (`TaskUsage`/`TurnUsageRecord` persist `cache_read_input_tokens` and `cache_creation_tokens`; analytics tabs surface cache-token totals). Remaining work is the *derived* metrics and per-turn surfacing below.

**Problem**: Wallfacer records `cache_read_input_tokens` and `cache_creation_tokens` per turn but does not compute or surface cache efficiency metrics.

**Design**:

Extend `TurnUsageRecord` (`internal/store/models.go`) with derived metrics computed at recording time:

```go
type TurnUsageRecord struct {
    // ... existing fields ...

    // Derived cache metrics (computed at append time).
    CacheHitRate    float64 `json:"cache_hit_rate"`     // cache_read / (cache_read + input + cache_creation)
    CacheBreak      bool    `json:"cache_break"`        // true if cache_read dropped >5% vs previous turn
    EffectiveRate   float64 `json:"effective_rate_usd"`  // cost / (input + cache_read + output), actual $/token
}
```

Surface in the UI (`frontend/src/components/analytics/`):
- Per-turn cache hit rate in a turn-usage timeline (the per-turn token breakdown data already exists).
- Cache break indicator (warning icon) on turns where cache was invalidated.
- Task-level aggregate: average cache hit rate, number of cache breaks, wasted cost from cache misses.
- Dashboard widget: across all tasks, cache hit rate trend over time.

### 1.2 Cache Break Diagnosis

When a cache break is detected (cache reads drop >5% AND >2,000 tokens between consecutive turns of the same session):

1. Record the break in a `CacheBreakRecord` appended to the turn log.
2. Capture what changed between turns: did AGENTS.md change? Did the model change? Was this the first turn after `--resume`? Did board.json content change?
3. Surface in the task timeline as a diagnostic event.

**Real-time signal via hooks**: If Claude Code lifecycle hooks are enabled, `PreCompact`/`PostCompact` hook events provide an immediate signal that context compaction occurred, a leading indicator of cache pressure. These events can supplement the heuristic detection above with ground-truth compaction data.

This requires capturing some inputs alongside outputs. Candidate approach: hash the AGENTS.md (delivered via `WALLFACER_INSTRUCTIONS_PATH`), board.json, and model string before each turn's exec, store in the turn record, and diff against the previous turn.

```go
type TurnContext struct {
    AgentsMD5   string `json:"agents_md5"`
    BoardMD5    string `json:"board_md5,omitempty"`
    Model       string `json:"model"`
    HasResume   bool   `json:"has_resume"`
    SessionID   string `json:"session_id,omitempty"`
}
```

### 1.3 --resume Cache Interaction Audit

**Question to answer**: Does `--resume` in wallfacer's per-turn exec model cause cache invalidation?

Claude Code's `--resume` reloads a prior session's message history. The cache key is the API request prefix (system prompt + tools + message history). If the session file is loaded byte-identically and the system prompt hasn't changed, cache should hit. But:

- Wallfacer composes AGENTS.md instructions before each turn (delivered via `--append-system-prompt` / `WALLFACER_INSTRUCTIONS_PATH`, see `internal/runner/container.go`). If the composed instructions change between turns, the dynamic region changes and cache is invalidated.
- Wallfacer refreshes board.json before each turn. If board.json is read by the agent and ends up in conversation history, subsequent turns have different conversation prefixes.
- The host-installed Claude Code may be a different version than the one that created the session (after a host upgrade).

**Investigation tasks**:

1. Instrument wallfacer to log `cache_read_input_tokens` ratio on the first turn after `--resume` vs first turn of a fresh session. If resume turns consistently show 0 cache reads, caching is broken.
2. Test whether AGENTS.md changes between turns cause cache breaks by comparing runs with stable vs changing AGENTS.md.
3. Test whether the standalone-binary sentinel bug affects wallfacer's host install (check whether the resolved `claude` binary is npx or the standalone build).

**Mitigation options** (apply based on findings):

- **Freeze AGENTS.md per session**: Only re-read AGENTS.md at session start, not between feedback rounds. Trade stale instructions for cache stability.
- **Prefer npx on the host**: If the sentinel bug applies, document/recommend installing `npx @anthropic-ai/claude-code` rather than the standalone binary, and surface a warning when the resolved binary looks like the standalone build.
- **Strip MCP connectors**: Ensure the host MCP config used for task execution has no cloud MCP connectors configured.
- **Version-pin Claude Code**: Pin or record the host Claude Code version per session for reproducibility and known-good caching.

### 1.4 Session Continuity vs Fresh Start Trade-off

Current behavior: tasks accumulate conversation history across turns via `--resume`. As conversations grow, they hit compaction. Compaction direction matters:

- "From" compaction (default): Summarizes old messages, keeps recent. Preserves cache prefix, good.
- "Up_to" compaction: Summarizes recent, keeps old. Invalidates cache, bad.

Wallfacer cannot control which direction Claude Code picks. But it can control *session length*:

**Design decision**: Add a `max_turns_per_session` config (default: unlimited, recommended: experiment with 10-20). When reached, the next turn starts a fresh session instead of `--resume`. This bounds context growth and gives predictable cache behavior at the cost of losing conversation history.

Expose as a per-task option and a global default in config (`internal/envconfig/`):

```
WALLFACER_MAX_TURNS_PER_SESSION: maximum turns before forcing a fresh session (0 = unlimited)
```

---

## Part 2: Token Reduction Infrastructure

### 2.1 Shell Output Compression

**Approach**: Integrate RTK-style output compression as a host-side layer around the agent process.

RTK intercepts Bash commands and returns compressed output. It claims 60-90% token savings on common operations (test output, git status, file listings). However, Claude Code's built-in tools (Read, Grep, Glob) bypass Bash and cannot be intercepted.

Because tasks run as host processes in a worktree (no image), RTK is a host PATH / wrapper concern, not an image-build artifact. The agent inherits the host environment that `HostBackend` constructs (`internal/executor/host.go`).

**Integration options**:

| Option | Mechanism | Coverage | Complexity |
|--------|-----------|----------|------------|
| A. RTK on host PATH | `rtk` available on `$PATH`, Bash hook in the user's shell rc / Claude settings | Bash tool only | Low |
| B. Claude Code hooks (PreToolUse) | `.claude/hooks/PreToolUse.sh` (or server-driven hook) rewrites Bash commands | Bash tool | Low |
| C. Bash wrapper on the agent's PATH | Prepend a wrapper `bash` (via the env `HostBackend` builds) that pipes through a compressor | All shell invocations | Medium |
| D. API proxy with response rewriting | MITM proxy that compresses tool results before they reach the API | All tools | High |

**Recommended**: Option B (Claude Code hooks via PreToolUse) once hook infrastructure is in place. A hook-based output-compression handler implements exactly this: wallfacer rewrites Bash commands to pipe through RTK before the agent sees the output. This keeps compression policy server-controlled rather than baked into the host environment.

**Fallback**: Option A (RTK on the host PATH) can ship independently as a low-risk interim step before hooks are available. The two approaches are compatible; host-level RTK can be replaced by hook-level rewriting without agent-visible changes.

**Implementation**:

1. *With hooks*: The hook handler's `PreToolUse` path rewrites Bash commands with RTK compression pipes. Controlled by `WALLFACER_HOOKS_COMPRESSION` env var.
2. *Without hooks*: Ensure `rtk` is on the agent's `$PATH` and configure shell interception in the environment `HostBackend` exports for the task process. Controlled by `WALLFACER_TOKEN_COMPRESSION` env var.
3. Track compressed vs uncompressed token counts to measure savings.

### 2.2 Board Context Optimization

**Status**: Partly shipped. `internal/runner/board.go` already limits sibling task text fields in the manifest and warns on oversized manifests (`logBoardManifestSizeWarning`). Remaining work is conditional generation and hash-based regeneration below.

Wallfacer generates `board.json` before each turn containing board state (sibling tasks, statuses, branch names, worktree paths). It is written to a temp dir and surfaced to the agent.

**Current waste**: The agent reads the board even when it doesn't need sibling context. The board refreshes before every turn, potentially contributing to cache invalidation if the content changes.

**Optimizations** (remaining):

1. **Lazy board generation**: Don't generate/surface board.json by default. Only generate it when the task has `mount_worktrees` enabled or the task prompt references sibling tasks.
2. **Stable board hashing**: Only regenerate board.json if the content actually changed (hash comparison). Avoid unnecessary file writes that change mtime.

### 2.3 AGENTS.md Size Management

AGENTS.md is composed into every turn's system context (via `--append-system-prompt` / `WALLFACER_INSTRUCTIONS_PATH`). Larger AGENTS.md means more dynamic tokens, hence more cost per turn.

**Optimizations**:

1. **Size tracking**: Log AGENTS.md size in bytes alongside turn usage. Surface in cache telemetry. (The composed-instructions hash is already recorded as `InstructionsHash` in `ExecutionEnvironment`; extend with a byte size.)
2. **Size warning**: Warn in the UI when AGENTS.md exceeds a threshold (e.g., 10KB) with estimated per-turn cost impact. (Mirrors the existing board-size warning pattern.)
3. **Per-task instruction subset**: Allow tasks to specify which sections of AGENTS.md they need (via tags or sections), and compose a filtered version.

---

## Part 3: Consumption Regression Model

### 3.1 Expected Cost Model

Build an internal regression model that predicts expected token consumption for a turn based on observable inputs:

**Features**:
- Turn number within session (later turns have more context)
- AGENTS.md size (tokens)
- Number of worktrees mounted
- Harness (Claude vs Codex)
- Model (Sonnet vs Opus, different context windows and pricing)
- Whether this is a `--resume` turn
- Task prompt length
- Previous turn's output size

**Target**: Total cost (USD) for the turn.

**Implementation**: Start simple, linear regression on historical turn data. Store coefficients in the server. Use the model for:

1. **Anomaly detection**: Flag turns where actual cost exceeds predicted cost by >3x. Surface as a warning event.
2. **Prospective budgeting**: Before starting a task, estimate total cost based on prompt length and expected turn count. Show in the task creation UI.
3. **Auto-pause on anomaly**: Optionally pause the task when a turn's cost is anomalous (exceeds prediction by configurable multiplier), allowing the user to inspect before continuing.

```
WALLFACER_COST_ANOMALY_MULTIPLIER: pause task if turn cost exceeds prediction times this value (0 = disabled, default: 0)
```

### 3.2 Historical Analysis Tooling

Add a CLI command (`internal/cli/`) for offline analysis:

```bash
wallfacer usage-report                    # Aggregate cost report across all tasks
wallfacer usage-report --task <id>        # Per-turn breakdown for a specific task
wallfacer usage-report --cache-analysis   # Cache hit rate analysis across all tasks
wallfacer usage-report --anomalies        # List turns with anomalous cost
```

This reads from existing `turn-usage.jsonl` files and `summary.json` snapshots. No new data collection needed, just analysis tooling over what wallfacer already records.

---

## Part 4: Budget Intelligence

### 4.1 Prospective Cost Estimation

Before a task starts, estimate its cost range based on:

- Prompt complexity (token count of prompt + AGENTS.md)
- Historical cost of similar tasks (same workspace, similar prompt length)
- Selected model pricing
- Expected turn count (from historical average for the workspace)

Show in the task creation UI as: "Estimated cost: $0.50 - $2.00 (based on 12 similar tasks)".

### 4.2 Live Budget Tracking with In-Context Warnings

Current behavior: the budget check happens after each turn, comparing accumulated cost against `MaxCostUSD` (`internal/runner/execute.go`). This is reactive, the task may have already overspent significantly on the final turn.

**Enhancement** (inspired by [cc-budget](https://github.com/boyand/cc-budget)'s threshold hook): After each turn, compute remaining budget percentage and check threshold crossings. When a threshold is crossed *for the first time*, inject a terse warning into the next turn's prompt context. The warning must be short (<20 tokens) to avoid wasting the budget it's trying to protect.

**Threshold levels** (configurable per-workspace):

| Threshold | Default | Injected message |
|-----------|---------|------------------|
| Warn      | 80%     | `[Budget: 80% used. Wrap up current work.]` |
| Critical  | 95%     | `[Budget: 95% used. Finish immediately.]` |

**Implementation**: The runner already computes accumulated cost per turn. Add a `budgetWarned` map keyed by threshold level to avoid repeated injection. On each turn completion:

1. Compute `usedPct = accumulatedCost / MaxCostUSD * 100`.
2. If `usedPct` crosses a threshold not yet warned, set the flag and store the warning message for injection into the next `--resume` turn's prompt prefix.
3. Warnings are injected via the existing feedback/continuation mechanism, appended to the prompt that resumes the session. The warning text can live as a template in `internal/prompts/`.

This mirrors cc-budget's `UserPromptSubmit` hook pattern but adapted for wallfacer's execution model where wallfacer controls the prompt, not a local hook.

**Alternative via hooks**: If Claude Code lifecycle hooks are enabled, budget warnings can instead be injected through the `UserPromptSubmit` hook's `additionalContext` field, exactly as cc-budget does. This is more natural (the agent sees it as part of the prompt context, not as a continuation message) but requires the hooks infrastructure. The runner-side approach above works without hooks and serves as the default.

### 4.3 Per-Turn Cost Deltas

Surface the cost delta for each turn prominently in the UI (inspired by cc-budget's `(+$X.XX)` display). The data already exists in `TurnUsageRecord`, compute the delta and display it:

- In the task card: show the last turn's cost delta, e.g., `Last turn: +$0.12`.
- In the turn-usage timeline: each row shows its absolute cost and the delta from the previous turn.
- Color-code: green for below-average turns, yellow for 2x average, red for 3x+ average.

### 4.4 Burn-Rate Pacing

For tasks with a `MaxCostUSD` budget, compute and display a pacing indicator (inspired by cc-budget's progress bar with pacing marker):

- **Burn rate**: `accumulatedCost / elapsedTurns`, average cost per turn.
- **Projected total**: `burnRate * estimatedTotalTurns` (estimated from historical average or linear extrapolation).
- **Pacing marker**: In a budget progress bar, show where the task *should* be if spending evenly across its expected lifetime. If actual spending is ahead of the pacing marker, the task is over-budget-pace.

Display as a compact progress bar in the task card:

```
Budget: ████████░░░░ 65% ($1.30/$2.00)  pace: 50% ← over-pace
```

### 4.5 Cross-Task Cost Attribution

For multi-task workflows (batch creation, dependency chains), track aggregate cost across the group. Surface in the UI as a "batch cost" alongside individual task costs.

### 4.6 Aggregate Cost Ledger

Track costs across all tasks aggregated by day and month (inspired by cc-budget's daily/monthly ledger). This goes beyond per-task tracking to give workspace-level spend visibility. Note `/api/stats` already exposes a per-day cost timeline; the ledger formalizes and persists this with longer retention and trend projection.

- **Daily ledger**: Total cost per day, broken down by harness (Claude vs Codex) and model.
- **Monthly ledger**: Running monthly total with trend projection ("on pace for $X this month").
- **Retention**: Daily entries retained for 90 days, monthly entries indefinitely.
- **Storage**: Append to a workspace-level `usage-ledger.jsonl` file (separate from per-task turn-usage files) in `internal/store/`.

Surface in the dashboard as a cost chart with daily bars and a monthly trend line.

### 4.7 Peak/Off-Peak Scheduling Awareness

Anthropic's rate limits are tighter during peak hours (America/Los_Angeles, roughly 5-11 AM PT weekdays, per cc-budget's detection logic). Wallfacer's auto-promoter can use this:

- **Display**: Show peak/off-peak status and countdown in the UI header.
- **Throttling** (opt-in): During peak hours, reduce `WALLFACER_MAX_PARALLEL` by a configurable factor (e.g., `WALLFACER_PEAK_THROTTLE_FACTOR=0.5` halves concurrency during peak).
- **Scheduling preference**: When multiple backlog tasks are eligible for promotion, prefer cheaper/smaller tasks during peak and defer expensive tasks to off-peak.

```
WALLFACER_PEAK_THROTTLE_FACTOR: multiplier for max_parallel during peak hours (0 = no throttling, default: 0)
```

### 4.8 Enterprise Discount

Support a configurable discount multiplier for organizations with negotiated Anthropic pricing:

```
WALLFACER_ENTERPRISE_DISCOUNT: percentage discount on list pricing for cost display (default: 0)
```

When set, all cost displays (per-turn, per-task, daily/monthly ledger, prospective estimates) apply the discount. The raw API-reported costs are stored unchanged; the discount is applied only at display time.

---

## Dependencies

- None required (builds on existing infrastructure: `TaskUsage`/`TurnUsageRecord`, `/api/usage`, `/api/stats`, and the analytics tabs).
- Hook-based delivery (Claude Code lifecycle hooks) is a complementary but optional mechanism for output compression (§2.1), cache break detection (§1.2), anomaly telemetry (§3.1), and budget-warning injection (§4.2). This spec works via runner-side heuristics without hooks.

## Acceptance Criteria

1. Cache hit rate is visible per-turn and per-task in the UI (extends the existing analytics tabs).
2. Cache breaks are detected and diagnosed with probable cause.
3. `--resume` cache behavior is documented with empirical data.
4. RTK or equivalent compression is available as an opt-in host-PATH / hook feature.
5. Board.json regeneration is conditional (hash-based) and lazy.
6. Usage report CLI command works on historical data.
7. Cost anomaly detection flags turns exceeding 3x predicted cost.
8. Prospective cost estimation appears in task creation UI.
9. Threshold-based budget warnings are injected into agent context at 80% and 95% (configurable), once per crossing.
10. Per-turn cost deltas are visible in the task card and turn-usage timeline.
11. Burn-rate pacing indicator appears on tasks with a `MaxCostUSD` budget.
12. Daily/monthly cost ledger aggregates workspace-level spend with trend projection.
13. Peak/off-peak status is displayed; optional auto-promoter throttling during peak hours.
14. Enterprise discount multiplier applies to all cost displays when configured.

## Non-Goals

- Modifying Claude Code's internal caching logic (we can only control inputs).
- Building a custom LLM proxy (too complex for the value; RTK-style compression is sufficient).
- Real-time token streaming (we get totals per invocation, not per-token).
- Negotiating different API pricing tiers.

## Open Questions

1. Does the sentinel replacement bug in the standalone Bun binary affect wallfacer's host install? Need to check whether the resolved `claude` binary is npx or the standalone build.
2. What is the actual cache hit rate for `--resume` turns in wallfacer today? Requires empirical measurement.
3. Is RTK's compression lossy in ways that hurt agent accuracy? Need to test on wallfacer's typical workloads.
4. Should the regression model be per-workspace or global? Per-workspace captures project-specific patterns but needs more data.
5. What is the right `max_turns_per_session` default? Too low wastes context; too high hits compaction overhead.
6. Are Anthropic's peak hours (5-11 AM PT weekdays, per cc-budget) stable enough to hardcode, or should wallfacer detect them dynamically from rate-limit response headers?
7. Should budget threshold warnings be injected as system messages or appended to the user prompt? System messages may be more reliable but depend on Claude Code's prompt assembly order.
8. What ledger retention policy balances storage cost vs analytics value? cc-budget uses 48h snapshots / 31d ledger; wallfacer may want longer retention given its multi-task nature.
9. The cloud/executor specs replace `--backend` selection with `--executor`. This spec frames everything around the single host `HostBackend`; if a future executor (cloud worker) reintroduces an image, the RTK delivery and binary-variant questions (§2.1, §1.3) need to be revisited for that path. Coordinate the executor terminology with those specs rather than relabeling here.
