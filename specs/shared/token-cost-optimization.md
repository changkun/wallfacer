# Token & Cost Optimization

## Status

Not started

## Track

Shared

## Problem

Wallfacer launches Claude Code in ephemeral containers, accumulating significant token costs across tasks. Two orthogonal problems exist:

1. **Cache correctness under wallfacer's execution model.** Claude Code's prompt caching system relies on a static/dynamic boundary in system prompts. Wallfacer's per-container invocation model — especially `--resume` for feedback loops and multi-turn continuations — may not be exploiting caching optimally or may be triggering cache invalidation unknowingly. There is no visibility into whether cache hits are actually occurring.

2. **Infrastructure-level token reduction.** Even with perfect caching, the raw volume of tokens flowing through tool outputs (git status, test results, file reads) is unnecessarily large. Tools like [RTK](https://github.com/rtk-ai/rtk) demonstrate 60-90% reduction on common dev commands by compressing shell output before the agent sees it.

### Context: How Claude Code Caching Works

From [claude-design-docs](https://github.com/changkun/claude-design-docs):

- **Static/dynamic boundary**: System prompt splits at a sentinel. Static content (tool schemas, safety rules, coding instructions) gets `cache_control: { type: 'ephemeral', scope: 'global' }`. Dynamic content (memory, env info, CLAUDE.md, MCP instructions) follows without cache scope.
- **Cache pricing**: Cache reads cost 10% of full input price ($0.30/MTok vs $3/MTok on Sonnet). First call writes at 1.25x; subsequent calls read at 0.1x. After two calls, net positive.
- **Cache invalidation triggers**: Changes to system prompt bytes, tool schemas, model string, beta headers, CLAUDE.md content mid-session, MCP connector reconnection.
- **Compaction**: When context hits ~(window - 13K), auto-compact summarizes older messages. "From" direction preserves cache prefix; "up_to" direction invalidates it. Post-compaction restoration re-injects ~5-50K tokens of recently-accessed files and skills.
- **Fork agent sharing**: Sub-agents share the parent's cache via byte-identical system prompts and tool pools. Only the final directive diverges.

### Known Bugs (as of early 2026)

Two cache bugs have been [widely reported](https://www.theregister.com/2026/03/31/anthropic_claude_code_limits/):

1. **Sentinel replacement bug**: The standalone Bun binary does a string replacement on a billing sentinel (`cch=24b72`). It replaces the *first* occurrence in serialized JSON. If conversation history contains this string, the wrong instance is replaced, breaking cache prefix on every request. Workaround: use `npx @anthropic-ai/claude-code` instead of the standalone binary.

2. **MCP tool schema injection**: Cloud MCP connectors inject complete tool schemas into every API call regardless of usage. Ahrefs alone adds 100+ tool definitions (tens of thousands of tokens per message). Workaround: disconnect unused connectors.

Additionally, `--resume` has been reported to always break cache in some versions, though this may be version-specific.

### What Wallfacer Controls vs What It Doesn't

| Layer | Wallfacer controls | Claude Code controls |
|-------|-------------------|---------------------|
| Container launch flags | `--resume`, `--model`, `-p` | N/A |
| Mounted content | AGENTS.md, board.json, worktrees | N/A |
| Shell output volume | Can intercept via hooks/wrappers | Built-in tools bypass Bash |
| System prompt composition | N/A | Static/dynamic boundary |
| Cache control headers | N/A | `cache_control` blocks |
| Context compaction | N/A | Auto-compact triggers |
| Session state | Session ID persistence | Internal message history |
| MCP tools in container | MCP config in sandbox image | Tool schema serialization |
| Binary variant | Image build (npx vs standalone) | N/A |

---

## Design Goals

1. **Observability**: Know exactly what cache behavior each task turn is getting — cache hit rate, invalidation causes, cost breakdown by cached vs uncached tokens.
2. **Cache correctness**: Ensure wallfacer's execution model (container-per-turn, `--resume`, feedback loops, AGENTS.md mounts) does not unnecessarily invalidate caches.
3. **Token reduction**: Reduce raw token volume flowing through agent turns via output compression, without changing agent behavior.
4. **Regression modeling**: Build a predictive model of expected token consumption per turn to detect anomalous cost spikes early.
5. **Budget intelligence**: Move from post-hoc budget checks to prospective cost estimation.

---

## Part 1: Cache Observability & Correctness

### 1.1 Cache Telemetry

**Problem**: Wallfacer records `cache_read_input_tokens` and `cache_creation_input_tokens` per turn but does not compute or surface cache efficiency metrics.

**Design**:

Extend `TurnUsageRecord` with derived metrics computed at recording time:

```go
type TurnUsageRecord struct {
    // ... existing fields ...

    // Derived cache metrics (computed at append time).
    CacheHitRate    float64 `json:"cache_hit_rate"`     // cache_read / (cache_read + input + cache_creation)
    CacheBreak      bool    `json:"cache_break"`        // true if cache_read dropped >5% vs previous turn
    EffectiveRate   float64 `json:"effective_rate_usd"`  // cost / (input + cache_read + output) — actual $/token
}
```

Surface in the UI:
- Per-turn cache hit rate in the turn-usage timeline (already has per-turn token breakdown).
- Cache break indicator (warning icon) on turns where cache was invalidated.
- Task-level aggregate: average cache hit rate, number of cache breaks, wasted cost from cache misses.
- Dashboard widget: across all tasks, cache hit rate trend over time.

### 1.2 Cache Break Diagnosis

When a cache break is detected (cache reads drop >5% AND >2,000 tokens between consecutive turns of the same session):

1. Record the break in a `CacheBreakRecord` appended to the turn log.
2. Capture what changed between turns: did AGENTS.md change? Did the model change? Was this the first turn after `--resume`? Did board.json content change?
3. Surface in the task timeline as a diagnostic event.

This requires capturing some inputs alongside outputs. Candidate approach: hash the mounted AGENTS.md, board.json, and model string before each container invocation, store in the turn record, and diff against previous turn.

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

**Question to answer**: Does `--resume` in wallfacer's container model cause cache invalidation?

Claude Code's `--resume` reloads a prior session's message history. The cache key is the API request prefix (system prompt + tools + message history). If the session file is loaded byte-identically and the system prompt hasn't changed, cache should hit. But:

- Wallfacer remounts AGENTS.md before each turn (it goes into system context via CLAUDE.md hierarchy). If AGENTS.md was edited between turns, the dynamic region changes → cache invalidated.
- Wallfacer refreshes board.json before each turn. If board.json is read by the agent and ends up in conversation history, subsequent turns have different conversation prefixes.
- The sandbox image may use a different Claude Code version than the one that created the session (after image rebuild).

**Investigation tasks**:

1. Instrument wallfacer to log `cache_read_input_tokens` ratio on the first turn after `--resume` vs first turn of a fresh session. If resume turns consistently show 0 cache reads, caching is broken.
2. Test whether AGENTS.md changes between turns cause cache breaks by comparing runs with stable vs changing AGENTS.md.
3. Test whether the standalone binary sentinel bug affects wallfacer's container image (check if image uses npx or standalone binary).

**Mitigation options** (apply based on findings):

- **Freeze AGENTS.md per session**: Only re-read AGENTS.md at session start, not between feedback rounds. Trade stale instructions for cache stability.
- **Use npx in container image**: If the sentinel bug applies, switch the Dockerfile from standalone binary to `npx @anthropic-ai/claude-code`.
- **Strip MCP connectors**: Ensure sandbox images have no cloud MCP connectors configured.
- **Version-pin Claude Code**: Pin the Claude Code version in the container image to a known-good version for caching.

### 1.4 Session Continuity vs Fresh Start Trade-off

Current behavior: tasks accumulate conversation history across turns via `--resume`. As conversations grow, they hit compaction. Compaction direction matters:

- "From" compaction (default): Summarizes old messages, keeps recent. Preserves cache prefix → good.
- "Up_to" compaction: Summarizes recent, keeps old. Invalidates cache → bad.

Wallfacer cannot control which direction Claude Code picks. But it can control *session length*:

**Design decision**: Add a `max_turns_per_session` config (default: unlimited, recommended: experiment with 10-20). When reached, the next turn starts a fresh session instead of `--resume`. This bounds context growth and gives predictable cache behavior at the cost of losing conversation history.

Expose as a per-task option and a global default in config:

```
WALLFACER_MAX_TURNS_PER_SESSION — maximum turns before forcing a fresh session (0 = unlimited)
```

---

## Part 2: Token Reduction Infrastructure

### 2.1 Shell Output Compression

**Approach**: Integrate RTK-style output compression as a middleware layer inside sandbox containers.

RTK intercepts Bash commands and returns compressed output. It claims 60-90% token savings on common operations (test output, git status, file listings). However, Claude Code's built-in tools (Read, Grep, Glob) bypass Bash and cannot be intercepted.

**Integration options**:

| Option | Mechanism | Coverage | Complexity |
|--------|-----------|----------|------------|
| A. Install RTK in sandbox image | `rtk` binary in PATH, Bash hook in `.bashrc` | Bash tool only | Low |
| B. Claude Code hooks (PreToolUse) | `.claude/hooks/PreToolUse.sh` | Bash tool | Low |
| C. Custom Bash wrapper | Replace `/usr/bin/bash` with a wrapper that pipes through compressor | All shell invocations | Medium |
| D. API proxy with response rewriting | MITM proxy that compresses tool results before they reach the API | All tools | High |

**Recommended**: Option A (RTK in sandbox image) as the starting point. It's the lowest risk and covers the highest-volume token source (test output, build errors, git operations). Can be toggled via env var.

**Implementation**:

1. Add RTK binary to Claude and Codex sandbox Dockerfiles.
2. Configure Claude Code hooks in the sandbox image to intercept Bash calls via RTK.
3. Add `WALLFACER_TOKEN_COMPRESSION` env var (default: `true`).
4. Track compressed vs uncompressed token counts to measure savings.

### 2.2 Board Context Optimization

Wallfacer generates `board.json` before each turn containing the full board state (all sibling tasks, their statuses, prompts, and worktree paths). This is mounted and read by the agent.

**Current waste**: The agent reads the full board even when it doesn't need sibling context. The board refreshes before every turn, potentially contributing to cache invalidation if the content changes.

**Optimizations**:

1. **Lazy board loading**: Don't mount board.json by default. Only generate it when the task has `mount_worktrees` enabled or the task prompt references sibling tasks.
2. **Stable board hashing**: Only regenerate board.json if the content actually changed (hash comparison). Avoid unnecessary file writes that change mtime.
3. **Minimal board**: Strip task prompts and large fields from board.json. Include only: task ID, title, status, branch name. Full details available via the API if the agent needs them.

### 2.3 AGENTS.md Size Management

AGENTS.md is mounted read-only and included in every container's system context. Larger AGENTS.md → more dynamic tokens → more cost per turn.

**Optimizations**:

1. **Size tracking**: Log AGENTS.md size in bytes alongside turn usage. Surface in cache telemetry.
2. **Size warning**: Warn in the UI when AGENTS.md exceeds a threshold (e.g., 10KB) with estimated per-turn cost impact.
3. **Per-task instruction subset**: Allow tasks to specify which sections of AGENTS.md they need (via tags or sections), and mount a filtered version.

---

## Part 3: Consumption Regression Model

### 3.1 Expected Cost Model

Build an internal regression model that predicts expected token consumption for a turn based on observable inputs:

**Features**:
- Turn number within session (later turns have more context)
- AGENTS.md size (tokens)
- Number of worktrees mounted
- Sandbox type (Claude vs Codex)
- Model (Sonnet vs Opus — different context windows and pricing)
- Whether this is a `--resume` turn
- Task prompt length
- Previous turn's output size

**Target**: Total cost (USD) for the turn.

**Implementation**: Start simple — linear regression on historical turn data. Store coefficients in the server. Use the model for:

1. **Anomaly detection**: Flag turns where actual cost exceeds predicted cost by >3x. Surface as a warning event.
2. **Prospective budgeting**: Before starting a task, estimate total cost based on prompt length and expected turn count. Show in the task creation UI.
3. **Auto-pause on anomaly**: Optionally pause the task when a turn's cost is anomalous (exceeds prediction by configurable multiplier), allowing the user to inspect before continuing.

```
WALLFACER_COST_ANOMALY_MULTIPLIER — pause task if turn cost exceeds prediction × this value (0 = disabled, default: 0)
```

### 3.2 Historical Analysis Tooling

Add a CLI command for offline analysis:

```bash
wallfacer usage-report                    # Aggregate cost report across all tasks
wallfacer usage-report --task <id>        # Per-turn breakdown for a specific task
wallfacer usage-report --cache-analysis   # Cache hit rate analysis across all tasks
wallfacer usage-report --anomalies        # List turns with anomalous cost
```

This reads from existing `turn-usage.jsonl` files and `summary.json` snapshots. No new data collection needed — just analysis tooling over what wallfacer already records.

---

## Part 4: Budget Intelligence

### 4.1 Prospective Cost Estimation

Before a task starts, estimate its cost range based on:

- Prompt complexity (token count of prompt + AGENTS.md)
- Historical cost of similar tasks (same workspace, similar prompt length)
- Selected model pricing
- Expected turn count (from historical average for the workspace)

Show in the task creation UI as: "Estimated cost: $0.50 – $2.00 (based on 12 similar tasks)".

### 4.2 Live Budget Tracking

Current behavior: budget check happens after each turn, comparing accumulated cost against `MaxCostUSD`. This is reactive — the task may have already overspent significantly on the final turn.

**Enhancement**: After each turn, compute remaining budget and estimated turns remaining. If estimated remaining cost exceeds remaining budget, inject a warning into the next turn's prompt telling the agent to wrap up efficiently.

```
[System: You have approximately $0.30 remaining in your budget (~2-3 turns). 
Prioritize completing the most important remaining work.]
```

This gives the agent a chance to prioritize rather than being hard-cut mid-work.

### 4.3 Cross-Task Cost Attribution

For multi-task workflows (batch creation, dependency chains), track aggregate cost across the group. Surface in the UI as a "batch cost" alongside individual task costs.

---

## Dependencies

- None (builds on existing infrastructure).
- Optionally benefits from [telemetry-observability.md](telemetry-observability.md) for metric storage and dashboarding, but can ship independently with in-process computation.

## Acceptance Criteria

1. Cache hit rate is visible per-turn and per-task in the UI.
2. Cache breaks are detected and diagnosed with probable cause.
3. `--resume` cache behavior is documented with empirical data.
4. RTK or equivalent compression is available as an opt-in sandbox feature.
5. Board.json regeneration is conditional (hash-based).
6. Usage report CLI command works on historical data.
7. Cost anomaly detection flags turns exceeding 3x predicted cost.
8. Prospective cost estimation appears in task creation UI.
9. Live budget warning is injected when remaining budget is low.

## Non-Goals

- Modifying Claude Code's internal caching logic (we can only control inputs).
- Building a custom LLM proxy (too complex for the value; RTK-style compression is sufficient).
- Real-time token streaming (we get totals per invocation, not per-token).
- Negotiating different API pricing tiers.

## Open Questions

1. Does the sentinel replacement bug in the standalone Bun binary affect wallfacer's container image? Need to check the Dockerfile.
2. What is the actual cache hit rate for `--resume` turns in wallfacer today? Requires empirical measurement.
3. Is RTK's compression lossy in ways that hurt agent accuracy? Need to test on wallfacer's typical workloads.
4. Should the regression model be per-workspace or global? Per-workspace captures project-specific patterns but needs more data.
5. What is the right `max_turns_per_session` default? Too low wastes context; too high hits compaction overhead.
