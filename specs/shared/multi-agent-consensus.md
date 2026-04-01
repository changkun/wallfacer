---
title: Multi-Agent Consensus & Cross-Provider Verification
status: drafted
depends_on: [specs/shared/agent-abstraction.md]
affects: [internal/runner/, internal/store/, internal/sandbox/, internal/envconfig/, ui/]
effort: xlarge
created: 2026-04-01
updated: 2026-04-01
author: changkun
dispatched_task_id: null
---

# Multi-Agent Consensus & Cross-Provider Verification

## Problem Statement

Today every sub-agent activity (implementation, testing, oversight, etc.) is executed by a single provider — by default Claude. Even when a different sandbox is configured per activity, the decision of one agent is final. This creates two blind spots:

1. **Same-provider testing.** When Claude writes the code and Claude also runs the test verdict, both share the same model biases. A subtle bug that Claude's reasoning pattern misses in implementation will likely also be missed in verification. Cross-provider testing (e.g., Codex or Gemini verifying Claude's output) provides an independent perspective.

2. **Single-point-of-failure decisions.** Critical decisions — whether code is correct, whether a task is done, whether a commit message is accurate — rest on one agent's judgment. There is no mechanism for requiring agreement between multiple independent agents before a decision is accepted.

### Why This Matters Now

- The sandbox routing infrastructure already supports per-activity provider selection (4-tier hierarchy in `container.go`). But routing alone is passive — it doesn't enforce cross-checking.
- The agent-abstraction spec (dependency) unifies the role descriptor pattern, making it cheap to invoke the same logical role with different providers. This spec builds on that foundation.
- As wallfacer manages more ambitious tasks, the cost of undetected errors grows. Adversarial cross-checking is a reliability multiplier.

## Design Goals

- **G1 — Cross-provider verification by default for high-stakes activities.** Testing, oversight, and commit decisions should be verifiable by a different provider than the one that produced the work.
- **G2 — Multi-agent consensus protocol.** For designated activities, require N-of-M agents (potentially from different providers) to agree before a decision is accepted.
- **G3 — Disagreement resolution.** When agents disagree, provide a structured escalation path — automated tie-breaking, human review, or a third-party arbiter.
- **G4 — Provider-agnostic.** The consensus mechanism must work across Claude, Codex, Gemini, and future providers without assuming provider-specific capabilities.
- **G5 — Incremental adoption.** Each layer is independently useful. Cross-provider testing alone delivers value without requiring full consensus.

## Current Architecture (Relevant)

**Sandbox types:** `Claude`, `Codex` (string enum in `internal/sandbox/`). Extensible via `Normalize()` for forward-compatible unknown types.

**Per-activity routing:** `sandboxForTaskActivity()` resolves provider via 4-tier hierarchy: per-task-per-activity → per-task → env-per-activity → env-default → Claude.

**Sub-agent roles:** 7 routable activities (implementation, testing, refinement, title, oversight, commit_message, idea_agent). After agent-abstraction refactor, each is a role descriptor invoked via `runAgent()`.

**Key constraint:** Agents are stateless single-shot or multi-turn containers. They communicate with the orchestrator via stdout NDJSON. No inter-agent messaging exists today.

## Design Space

### Layer 1 — Cross-Provider Verification (Adversarial Testing)

The simplest valuable step: when a task completes implementation, run the test/verification activity using a *different* provider than the one that wrote the code.

#### 1A — Static Cross-Provider Routing

Configure a "verification provider" per activity that must differ from the implementation provider:

```
WALLFACER_CROSS_PROVIDER_TESTING=true
WALLFACER_SANDBOX_IMPLEMENTATION=claude
WALLFACER_SANDBOX_TESTING=codex
```

When `CROSS_PROVIDER_TESTING=true`, the runner validates at task start that `SANDBOX_TESTING != SANDBOX_IMPLEMENTATION` (or the resolved per-task values differ). If they match, the runner automatically selects an alternate provider for testing.

**Provider selection for cross-verification:**
- If implementation used Claude → testing uses Codex (or Gemini if configured)
- If implementation used Codex → testing uses Claude
- Configurable fallback chain: `WALLFACER_CROSS_PROVIDER_CHAIN=claude,codex,gemini`

**Pros:** Minimal change. Leverages existing routing. Immediate value.
**Cons:** Static — doesn't handle cases where you want *both* providers to test.

#### 1B — Dual Verification

Run the same verification activity with two providers independently, then compare results:

```
Implement (Claude) → Test (Claude) + Test (Codex) → Compare verdicts
```

If both pass: proceed.
If both fail: fail the task.
If they disagree: escalate (see Layer 3).

This is more expensive (2x test containers) but catches cases where one provider's test verdict is wrong.

**Verdict comparison:** The test activity already produces structured output (pass/fail + reasoning). Comparing verdicts is a string comparison on the structured result.

### Layer 2 — Multi-Agent Consensus

Generalize dual verification into a consensus protocol for any activity.

#### Consensus Modes

| Mode | Behavior | Use Case |
|------|----------|----------|
| `single` | One agent decides (current behavior) | Title generation, low-stakes activities |
| `cross` | One agent, but must be a different provider than the producer | Testing after implementation |
| `unanimous` | All N agents must agree | Commit message, merge decisions |
| `majority` | ⌈N/2⌉+1 agents must agree | Oversight verdicts |
| `any` | First agent to respond wins | Latency-sensitive activities |

#### Consensus Configuration

Per-activity consensus can be configured via env vars or per-task overrides:

```
WALLFACER_CONSENSUS_TESTING=cross
WALLFACER_CONSENSUS_OVERSIGHT=majority
WALLFACER_CONSENSUS_COMMIT_MESSAGE=unanimous
```

Or per-task in the API:

```json
{
  "consensus": {
    "testing": { "mode": "majority", "providers": ["claude", "codex", "gemini"] },
    "oversight": { "mode": "cross" }
  }
}
```

#### Consensus Group

A consensus group is a set of agent invocations for the same activity, differing only in provider:

```go
type ConsensusGroup struct {
    Activity   store.SandboxActivity
    Mode       ConsensusMode           // single, cross, unanimous, majority, any
    Providers  []sandbox.Type          // providers to invoke
    Producer   sandbox.Type            // which provider produced the artifact being verified
    Results    []ConsensusResult
}

type ConsensusResult struct {
    Provider  sandbox.Type
    Verdict   string        // structured verdict from agent output
    Reasoning string        // agent's reasoning (for disagreement analysis)
    Usage     store.TaskUsage
    Duration  time.Duration
}
```

#### Execution Flow

```
1. Runner resolves consensus config for the activity
2. If mode == "single": existing behavior (one agent, done)
3. If mode == "cross": select one provider ≠ producer, run once
4. If mode in {unanimous, majority}:
   a. Launch N agents in parallel (one per provider)
   b. Collect all results
   c. Apply consensus rule:
      - unanimous: all verdicts must match
      - majority: ⌈N/2⌉+1 matching verdicts required
   d. If consensus reached: accept the majority verdict
   e. If no consensus: escalate (Layer 3)
```

Parallel execution is natural here — agents are independent containers with no shared state. The runner already supports concurrent container launches.

### Layer 3 — Disagreement Resolution

When agents disagree and consensus is not reached:

#### 3A — Arbiter Agent

A designated arbiter agent (possibly a different provider or a more capable model) receives the disagreement context — all agents' verdicts and reasoning — and makes a final decision.

```go
type ArbiterConfig struct {
    Provider    sandbox.Type  // which provider arbitrates
    PromptTmpl  string        // template receives all verdicts + reasoning
    Timeout     time.Duration
}
```

The arbiter prompt template includes:

```
Activity: {{.Activity}}
Artifact under review:
{{.ArtifactSummary}}

Agent verdicts:
{{range .Results}}
Provider: {{.Provider}}
Verdict: {{.Verdict}}
Reasoning: {{.Reasoning}}
{{end}}

You are the arbiter. Analyze the disagreement and provide your final verdict.
Indicate which agent(s) you agree with and why.
```

**Pros:** Automated, fast, provides a decision.
**Cons:** The arbiter is itself a single agent — turtles all the way down. Mitigated by using a different provider as arbiter.

#### 3B — Human Escalation

When consensus fails and no arbiter is configured (or the arbiter also disagrees with both sides), escalate to the user:

- Transition task to `waiting` state with a structured disagreement report
- The disagreement report includes each agent's verdict and reasoning
- User can approve one verdict, request re-run, or provide manual feedback

This fits naturally into the existing waiting→feedback flow.

#### 3C — Configurable Escalation Chain

```
WALLFACER_CONSENSUS_ESCALATION=arbiter,human
```

1. First try arbiter agent
2. If arbiter can't decide (or is not configured), escalate to human

### Layer 4 — Provider Registry (Gemini and Beyond)

Currently only Claude and Codex are defined as sandbox types. To support Gemini and future providers:

#### Provider Extension

Extend the sandbox type system to support dynamic provider registration:

```go
// internal/sandbox/sandbox.go — already supports this via Normalize()
// Normalize("gemini") returns Type("gemini") without requiring a hardcoded constant

// New: provider capability declaration
type ProviderCapability struct {
    Type          Type
    Image         string   // container image
    CLIBinary     string   // path inside container
    PromptFlag    string   // how to pass the prompt (-p, --prompt, etc.)
    OutputFormat  string   // expected output format
    EnvVars       []string // required env vars (API keys, etc.)
    ModelFlag     string   // how to pass model selection
}
```

This is a lightweight extension — not a full plugin system. Each provider needs a container image that speaks the same NDJSON output protocol (or an adapter).

#### Gemini Integration Path

For Gemini specifically:
1. Build a `wallfacer-gemini` container image with a CLI wrapper that accepts `-p` prompts and emits NDJSON output compatible with the existing `agentOutput` parser
2. Register Gemini env vars: `GEMINI_API_KEY`, `GEMINI_DEFAULT_MODEL`
3. Add `gemini` as a sandbox type constant
4. The existing routing infrastructure handles the rest

The CLI wrapper is necessary because Google's Gemini doesn't have a direct equivalent of `claude -p` or `codex`. The wrapper translates between wallfacer's container protocol and the Gemini API.

## Data Model Changes

### Task Model

```go
type Task struct {
    // ... existing fields ...

    // Per-activity consensus configuration (overrides env defaults)
    Consensus map[SandboxActivity]ConsensusConfig `json:"consensus,omitempty"`
}

type ConsensusConfig struct {
    Mode       ConsensusMode   `json:"mode"`                  // single, cross, unanimous, majority, any
    Providers  []sandbox.Type  `json:"providers,omitempty"`   // explicit provider list (optional)
    Escalation []string        `json:"escalation,omitempty"`  // escalation chain: ["arbiter", "human"]
}
```

### Consensus Event Types

New event types for the task event timeline:

```go
const (
    EventConsensusStart    EventType = "consensus_start"     // consensus group launched
    EventConsensusVerdict  EventType = "consensus_verdict"   // individual agent verdict
    EventConsensusReached  EventType = "consensus_reached"   // agreement achieved
    EventConsensusFailed   EventType = "consensus_failed"    // disagreement, escalating
    EventArbiterVerdict    EventType = "arbiter_verdict"     // arbiter decision
)
```

### Usage Tracking

Consensus invocations create multiple `TurnUsageRecord` entries (one per provider) under the same activity, distinguished by the `Sandbox` field already present on the record. Aggregate cost includes all consensus participants.

## API Changes

### Task Creation/Update

The `POST /api/tasks` and `PATCH /api/tasks/{id}` endpoints accept the new `consensus` field:

```json
{
  "prompt": "...",
  "consensus": {
    "testing": { "mode": "cross" },
    "oversight": { "mode": "majority", "providers": ["claude", "codex", "gemini"] }
  }
}
```

### Consensus Status

`GET /api/tasks/{id}/events` already supports the new event types. No new endpoint needed — the event timeline shows the full consensus flow.

### Config Endpoint

`GET /api/config` returns the default consensus configuration.
`PUT /api/config` accepts consensus defaults alongside existing autopilot settings.

## UI Changes

### Task Card — Consensus Indicators

When a task activity uses consensus mode, the task card shows:
- A multi-provider badge (e.g., provider icons side by side) during consensus evaluation
- Green check if consensus reached, amber warning if escalated, red X if failed
- Expandable detail showing each provider's verdict

### Settings — Consensus Configuration

A new section in Settings for configuring default consensus modes per activity:

| Activity | Mode | Providers |
|----------|------|-----------|
| Testing | `cross` | (auto) |
| Oversight | `single` | (default) |
| Commit Message | `single` | (default) |

### Disagreement Review

When consensus fails and escalates to human:
- Task enters `waiting` state with a structured disagreement panel
- Panel shows side-by-side verdicts from each provider
- User can accept one verdict, request re-evaluation, or provide feedback

## Implementation Plan

### Phase 1 — Cross-Provider Testing (Layer 1A)

The minimum viable cross-checking. Delivers immediate value.

1. Add `WALLFACER_CROSS_PROVIDER_TESTING` env var (bool, default `false`).
2. Add `WALLFACER_CROSS_PROVIDER_CHAIN` env var (comma-separated provider list, default `claude,codex`).
3. In the test verification flow (`execute.go` / test activity), when cross-provider is enabled, resolve the testing sandbox to the next provider in the chain that differs from the implementation provider.
4. Add event logging for cross-provider selection.
5. Update the UI to show which provider performed testing (already partially visible via turn-usage sandbox field).

**Depends on:** Agent abstraction Phase 1 (role descriptors) for clean invocation.

### Phase 2 — Consensus Protocol (Layer 2)

Generalize cross-provider verification into configurable consensus.

1. Define `ConsensusMode` enum and `ConsensusConfig` struct in `internal/store/models.go`.
2. Add `Consensus` field to `Task` model.
3. Add consensus env var parsing to `internal/envconfig/`.
4. Implement `runConsensus(ctx, activity, task, prompt) (ConsensusResult, error)` in `internal/runner/`:
   - Resolves consensus config for the activity
   - For `single`/`cross`: delegates to existing `runAgent()`
   - For `majority`/`unanimous`: launches parallel agents, collects results, applies consensus rule
5. Add consensus event types to the event timeline.
6. Wire consensus into the test and oversight activities as opt-in.

### Phase 3 — Disagreement Resolution (Layer 3)

1. Implement arbiter agent role: a new role descriptor that receives disagreement context.
2. Add arbiter prompt template to `internal/prompts/`.
3. Implement escalation chain: arbiter → human (waiting state with disagreement report).
4. Add disagreement review UI panel.

### Phase 4 — Provider Registry (Layer 4)

1. Extend sandbox type system with `ProviderCapability` metadata.
2. Build `wallfacer-gemini` container image with NDJSON adapter.
3. Add Gemini env vars and sandbox constant.
4. Validate that consensus works across 3 providers.

## Cost Considerations

Multi-agent consensus multiplies per-activity cost:

| Mode | Cost Multiplier | When Justified |
|------|----------------|----------------|
| `single` | 1x | Low-stakes activities (title, commit msg) |
| `cross` | 1x | Same cost, different provider — no multiplier |
| `majority` (3 providers) | 3x | High-stakes verification |
| `unanimous` (3 providers) | 3x | Critical decisions |

Mitigation strategies:
- Default to `single` for everything; users opt in per activity
- Use `cross` (no cost multiplier) as the recommended default for testing
- Budget-aware consensus: skip multi-agent if task cost budget is tight
- Headless activities (title, commit msg) should rarely need consensus

## Open Questions

1. **Output format compatibility.** Claude Code and Codex emit different NDJSON schemas. The existing `agentOutput` parser handles both. Would Gemini need its own adapter, or can a container-level wrapper normalize the output? (Likely yes — wrapper approach.)

2. **Verdict comparability.** How do we compare verdicts across providers? For test pass/fail, it's binary. For oversight summaries, comparing free-text output is harder. Should consensus activities emit a structured verdict alongside prose?

3. **Latency impact.** Parallel consensus adds wall-clock time (slowest provider). Sequential consensus (for budget reasons) adds cumulative time. Is there a latency SLA?

4. **Provider bias correlation.** Cross-provider checking assumes independent failure modes. If all LLM providers share training data biases, cross-checking provides less value. How do we measure the actual independence?

5. **Consensus for implementation.** This spec focuses on verification/decision activities. Should the implementation itself ever require consensus (e.g., two agents implement independently, then a merge agent combines the best parts)? This is architecturally different and likely a separate spec.

6. **Interaction with auto-retry.** If consensus fails and the task is auto-retried, should the retry use the same consensus config or fall back to single-provider?

## Deferred Work

- **User-configurable consensus pipelines via UI.** Phase 2 supports config via env vars and API; a visual pipeline editor is a separate effort.
- **Dynamic provider selection based on task characteristics.** E.g., routing security-sensitive tasks to providers with better safety training. Requires task classification infrastructure.
- **Consensus for multi-turn implementation.** Requiring agreement during implementation (not just verification) is a fundamentally different problem — it requires inter-agent communication during execution, not just post-hoc comparison.
- **Formal verification integration.** Using deterministic tools (linters, type checkers, fuzz testers) as "agents" in the consensus group alongside LLM providers. These could count as votes in the consensus protocol.
