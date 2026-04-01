---
title: Multi-Agent Debate — Adversarial Deliberation for Ideation and Signal Triage
status: drafted
depends_on:
  - specs/shared/agent-abstraction.md
  - specs/shared/multi-agent-consensus.md
affects: [internal/runner/, internal/store/, internal/prompts/, ui/]
effort: xlarge
created: 2026-04-01
updated: 2026-04-01
author: changkun
dispatched_task_id: null
---

# Multi-Agent Debate — Adversarial Deliberation for Ideation and Signal Triage

## Problem Statement

The multi-agent consensus spec addresses *verification* — parallel, independent agents vote on a binary outcome (pass/fail, accept/reject). But two important activities demand something richer than a vote: they need *deliberation*.

### 1. Single-perspective ideation

The brainstorm agent (`RunIdeation`) runs a single provider in a single pass. It proposes ideas, self-critiques, and outputs the top 3. The self-critique is inherently limited — the same model that generates an idea is unlikely to find its own blind spots. A second model from a different provider would challenge assumptions, propose alternatives, and surface risks the first model's reasoning pattern systematically misses.

Today's ideation also has no mechanism for *building on* another agent's ideas. Agent A might propose "add caching" while Agent B (if it ran independently) might propose "add a CDN." Neither alone would discover the synthesis: "add edge caching with CDN fallback." Debate enables this emergent combination.

### 2. Single-perspective signal triage

The telemetry spec (dependency) introduces a signal-to-code feedback loop: runtime anomalies are detected, correlated to tasks, and auto-dispatched as fix tasks. But the triage decision — *what does this signal mean?* and *what should we do about it?* — is made by a single agent (or by threshold rules). This is fragile:

- A latency spike could be a code regression, a database issue, or a traffic burst. One agent's diagnosis depends on its training biases.
- An error rate increase could warrant a hotfix, a rollback, or just monitoring. The action depends on judgment that benefits from multiple perspectives.
- Correlated signals (latency + error rate + memory growth) require holistic reasoning that a single-pass agent may not synthesize.

### Why debate, not just consensus?

Consensus (from the sibling spec) is a *voting protocol*: agents independently produce answers, then a rule selects the winner. Debate is a *conversation protocol*: agents see each other's reasoning, respond to it, and iteratively refine their positions. The key differences:

| Aspect | Consensus | Debate |
|--------|-----------|--------|
| Interaction | Independent, parallel | Sequential, reactive |
| Communication | None between agents | Each round sees prior rounds |
| Convergence | Majority/unanimous vote | Argued agreement or explicit impasse |
| Strength | Fast, cheap, clear decision | Deeper reasoning, emergent synthesis |
| Cost | N× single invocation | N× R rounds (more expensive) |
| Best for | Binary/categorical decisions | Open-ended analysis, creative proposals |

## Design Goals

- **G1 — Structured multi-round debate.** Agents take turns responding to each other's arguments. Each round adds new information or refines prior positions. The protocol has a defined termination condition.
- **G2 — Cross-provider by default.** Each debate participant should be a different provider (Claude, Codex, Gemini) to maximize perspective diversity. Same-provider debate is allowed but provides less value.
- **G3 — Two concrete applications.** The spec must deliver working debate protocols for (a) ideation and (b) telemetry signal triage. The framework should be general enough for future applications.
- **G4 — Observable.** Every debate round is logged as a task event. The UI shows the debate transcript so users can understand how the conclusion was reached.
- **G5 — Bounded cost.** Debate has a hard round limit and an optional cost ceiling. Runaway debates are impossible.

## Design

### Debate Protocol

A debate is a structured multi-round conversation between N agents (typically 2-3) from different providers, mediated by the wallfacer orchestrator.

```
Round 0 (Seed):
  Orchestrator provides the debate topic + context to all participants.

Round 1 (Opening):
  Each participant produces an opening position.
  Positions are collected by the orchestrator.

Round 2..R (Rebuttal):
  Each participant receives all prior round transcripts and produces a response:
    - Challenges to other participants' positions
    - Concessions on points well-argued by others
    - Refined or synthesized proposals
  Orchestrator collects responses.

Round R+1 (Closing):
  Each participant produces a final position given the full transcript.
  Orchestrator evaluates convergence.

Resolution:
  If positions converge → accept the converged conclusion.
  If positions diverge → apply tiebreaker (arbiter agent, user escalation, or majority of final positions).
```

#### Convergence Detection

After the closing round, the orchestrator checks whether participants agree:

- **Ideation debates:** Convergence = participants endorse the same top-N ideas (by title/concept match, not exact wording). Partial convergence = overlapping subsets.
- **Signal triage debates:** Convergence = participants agree on (a) root cause diagnosis and (b) recommended action. Partial convergence = agree on diagnosis but differ on action.

Convergence can be evaluated by a lightweight headless agent (the "judge") that reads all final positions and outputs a structured verdict: `{converged: bool, agreed_points: [...], disagreed_points: [...]}`.

#### Data Model

```go
// Debate represents a structured multi-round deliberation between agents.
type Debate struct {
    ID            uuid.UUID            `json:"id"`
    TaskID        uuid.UUID            `json:"task_id"`           // parent task
    Activity      store.SandboxActivity `json:"activity"`         // ideation, triage, etc.
    Topic         string               `json:"topic"`             // debate prompt/question
    Participants  []DebateParticipant   `json:"participants"`
    Rounds        []DebateRound         `json:"rounds"`
    Config        DebateConfig          `json:"config"`
    Status        DebateStatus          `json:"status"`           // running, converged, diverged, cancelled
    Conclusion    *DebateConclusion     `json:"conclusion,omitempty"`
    StartedAt     time.Time            `json:"started_at"`
    CompletedAt   *time.Time           `json:"completed_at,omitempty"`
}

type DebateParticipant struct {
    ID       string        `json:"id"`       // "claude", "codex", "gemini"
    Provider sandbox.Type  `json:"provider"`
    Role     string        `json:"role"`     // "proposer", "critic", "synthesizer" (optional)
}

type DebateRound struct {
    Number     int                  `json:"number"`
    Phase      DebatePhase          `json:"phase"`    // opening, rebuttal, closing
    Responses  []DebateResponse     `json:"responses"`
    StartedAt  time.Time            `json:"started_at"`
    Duration   time.Duration        `json:"duration"`
}

type DebateResponse struct {
    ParticipantID string          `json:"participant_id"`
    Content       string          `json:"content"`       // free-text argument
    Structured    json.RawMessage `json:"structured"`    // activity-specific parsed output
    Usage         store.TaskUsage `json:"usage"`
}

type DebateConclusion struct {
    Converged      bool              `json:"converged"`
    AgreedPoints   []string          `json:"agreed_points"`
    DisagreedPoints []string         `json:"disagreed_points,omitempty"`
    FinalResult    json.RawMessage   `json:"final_result"`  // activity-specific (ideas, triage action)
    ResolvedBy     string            `json:"resolved_by"`   // "convergence", "arbiter", "user", "majority"
}

type DebateConfig struct {
    MaxRounds      int              `json:"max_rounds"`       // hard ceiling (default: 3)
    MaxCostUSD     float64          `json:"max_cost_usd"`     // cost ceiling (0 = no limit)
    Providers      []sandbox.Type   `json:"providers"`        // participants
    TurnOrder      TurnOrder        `json:"turn_order"`       // simultaneous, round_robin, or random
    Escalation     []string         `json:"escalation"`       // on divergence: ["arbiter", "user"]
}

type DebatePhase string
const (
    DebatePhaseOpening  DebatePhase = "opening"
    DebatePhaseRebuttal DebatePhase = "rebuttal"
    DebatePhaseClosing  DebatePhase = "closing"
)

type TurnOrder string
const (
    TurnSimultaneous TurnOrder = "simultaneous"  // all respond in parallel per round
    TurnRoundRobin   TurnOrder = "round_robin"    // A→B→C→A→B→C
    TurnRandom       TurnOrder = "random"          // random order each round
)

type DebateStatus string
const (
    DebateRunning   DebateStatus = "running"
    DebateConverged DebateStatus = "converged"
    DebateDiverged  DebateStatus = "diverged"
    DebateCancelled DebateStatus = "cancelled"
)
```

### Prompt Construction

Each debate round constructs a prompt that includes:

1. **Debate context** — the topic, activity-specific data (workspace signals, telemetry data, etc.)
2. **Participant role** — optional role assignment (proposer, critic, synthesizer)
3. **Transcript** — all prior rounds' responses from all participants
4. **Round instructions** — phase-specific guidance (opening: state your position; rebuttal: respond to others; closing: state your final position)

```
You are participating in a structured debate with other AI agents.
Your provider: {{.Provider}}
Your role: {{.Role}}

## Topic
{{.Topic}}

## Context
{{.ActivityContext}}

## Debate Transcript
{{range .PriorRounds}}
### Round {{.Number}} ({{.Phase}})
{{range .Responses}}
**{{.ParticipantID}}:** {{.Content}}
{{end}}
{{end}}

## Your Task (Round {{.CurrentRound}}, {{.Phase}})
{{.PhaseInstructions}}

Respond with your position. Be specific and concrete.
When you disagree with another participant, explain why with evidence.
When you agree, say so explicitly and build on their reasoning.
```

The prompt template lives in `internal/prompts/debate.tmpl` with activity-specific variants (`debate-ideation.tmpl`, `debate-triage.tmpl`).

### Orchestrator Execution Flow

```go
func (r *Runner) runDebate(ctx context.Context, taskID uuid.UUID, config DebateConfig, topic string, activityCtx any) (*Debate, error) {
    debate := &Debate{
        ID:           uuid.New(),
        TaskID:       taskID,
        Topic:        topic,
        Config:       config,
        Status:       DebateRunning,
        Participants: buildParticipants(config.Providers),
        StartedAt:    time.Now(),
    }

    // Round 0: Opening — all participants respond to the seed topic
    opening := r.runDebateRound(ctx, debate, DebatePhaseOpening)
    debate.Rounds = append(debate.Rounds, opening)

    // Rounds 1..N-1: Rebuttal — each sees the full transcript so far
    for round := 1; round < config.MaxRounds-1; round++ {
        if r.debateCostExceeded(debate, config.MaxCostUSD) {
            break
        }
        rebuttal := r.runDebateRound(ctx, debate, DebatePhaseRebuttal)
        debate.Rounds = append(debate.Rounds, rebuttal)

        // Early termination: if positions are already converging, skip remaining rounds
        if r.detectEarlyConvergence(debate) {
            break
        }
    }

    // Final round: Closing — participants state final positions
    closing := r.runDebateRound(ctx, debate, DebatePhaseClosing)
    debate.Rounds = append(debate.Rounds, closing)

    // Evaluate convergence
    conclusion := r.evaluateDebate(ctx, debate)
    debate.Conclusion = &conclusion

    if conclusion.Converged {
        debate.Status = DebateConverged
    } else {
        debate.Status = DebateDiverged
        // Apply escalation chain
        r.escalateDebate(ctx, debate)
    }

    return debate, nil
}
```

Within each round, execution depends on `TurnOrder`:

- **Simultaneous:** Launch all participants in parallel (like consensus). Cheapest wall-clock time. Each participant sees the same transcript (prior rounds only, not current round peers). This is the default.
- **Round-robin:** Participants respond sequentially within a round. Later respondents in the same round see earlier respondents' current-round output. Richer interaction but slower.
- **Random:** Like round-robin but shuffled order each round, preventing positional bias (first-mover advantage).

### Application 1: Ideation Debate

Replace or augment the single-agent brainstorm with a multi-agent debate.

#### Flow

```
Seed: "Analyze these workspaces and propose improvement ideas."
     + workspace signals (failures, churn hotspots, TODOs)
     + existing tasks (to avoid duplicates)
     + rejected ideas history

Opening (parallel):
  Claude → proposes 3-5 ideas with rationale
  Codex  → proposes 3-5 ideas with rationale
  (Gemini → proposes 3-5 ideas with rationale, if configured)

Rebuttal 1:
  Each agent sees all opening proposals and:
  - Critiques weak proposals (vague scope, low impact, duplicates existing work)
  - Champions strong proposals from other agents
  - Proposes synthesis of complementary ideas
  - May introduce new ideas inspired by others' proposals

Rebuttal 2 (if budget allows):
  Agents converge toward a shared ranking, making final arguments

Closing:
  Each agent outputs a ranked top-3 list with confidence scores

Resolution:
  Judge agent (or simple ranking aggregation) produces the final top-3
  Ideas that appear in multiple agents' top-3 are ranked highest
```

#### Integration with Existing Ideation

The debate replaces `RunIdeation` when `WALLFACER_IDEATION_MODE=debate`:

| Mode | Behavior | Cost |
|------|----------|------|
| `single` | Current behavior: one agent, self-critique | 1× |
| `debate` | Multi-agent debate with configurable rounds | N×R (N providers × R rounds) |

When `mode=debate`, `runIdeationTask` calls `runDebate()` instead of `RunIdeation()`. The output format is the same (`[]IdeateResult`) — the debate produces ideas compatible with the existing backlog-task-creation flow.

#### Debate Ideation Prompt Template

The ideation-specific debate prompt extends the base debate template with:
- Workspace mount paths and file structure context
- Failure signals, churn hotspots, TODO signals (same as today)
- Rejected ideas history (same as today)
- Existing task list (same as today)

The activity-specific structured output schema remains `IdeateResult[]`.

### Application 2: Telemetry Signal Triage Debate

When the telemetry system detects an anomaly (from the telemetry spec), instead of a single agent deciding the action, a debate determines (a) root cause and (b) recommended action.

#### Flow

```
Seed: "Analyze this runtime anomaly and recommend an action."
     + anomaly data (type, severity, evidence)
     + telemetry context (error logs, latency data, recent changes)
     + correlated task/commit information
     + workspace code context (relevant files from git blame)

Opening (parallel):
  Claude → diagnosis + recommended action + confidence
  Codex  → diagnosis + recommended action + confidence

Rebuttal:
  Each agent sees the other's diagnosis and:
  - Challenges if they disagree on root cause
  - Proposes additional evidence to investigate
  - Refines the action plan (hotfix vs rollback vs monitor)

Closing:
  Each agent outputs final: {diagnosis, action, confidence, evidence}

Resolution:
  If diagnoses agree → execute the agreed action
  If diagnoses differ → escalate to user with both analyses
```

#### Triage Actions

The debate concludes with a structured triage decision:

```go
type TriageDecision struct {
    Diagnosis      string          `json:"diagnosis"`       // root cause explanation
    Confidence     float64         `json:"confidence"`      // 0.0-1.0
    Action         TriageAction    `json:"action"`          // what to do
    ActionDetail   string          `json:"action_detail"`   // specific steps
    Evidence       []string        `json:"evidence"`        // supporting data points
}

type TriageAction string
const (
    TriageHotfix   TriageAction = "hotfix"    // create a fix task
    TriageRollback TriageAction = "rollback"  // revert the correlated task's changes
    TriageMonitor  TriageAction = "monitor"   // watch for N more minutes before acting
    TriageIgnore   TriageAction = "ignore"    // false positive, dismiss the anomaly
    TriageEscalate TriageAction = "escalate"  // needs human judgment
)
```

#### Integration with Telemetry Auto-Dispatch

The telemetry spec's auto-dispatch flow gains a debate step:

```
Anomaly detected
    │
    ├─ WALLFACER_TRIAGE_MODE=single → single agent decides (current telemetry spec)
    │
    └─ WALLFACER_TRIAGE_MODE=debate → debate between agents
         │
         ├─ Converged on "hotfix" → auto-create fix task
         ├─ Converged on "rollback" → trigger task revert flow
         ├─ Converged on "monitor" → suppress auto-dispatch, re-evaluate later
         ├─ Converged on "ignore" → dismiss anomaly
         └─ Diverged → escalate to user (task enters waiting state)
```

This adds a deliberation layer between signal detection and action, reducing false-positive auto-dispatches.

### Debate Event Types

New event types for the task event timeline:

```go
const (
    EventDebateStart     EventType = "debate_start"      // debate initiated
    EventDebateRound     EventType = "debate_round"       // round completed, contains all responses
    EventDebateConverge  EventType = "debate_converge"    // convergence detected
    EventDebateDiverge   EventType = "debate_diverge"     // divergence, escalating
    EventDebateEnd       EventType = "debate_end"         // debate concluded with result
)
```

Each `debate_round` event contains the full round transcript, enabling the UI to reconstruct the conversation.

### Usage Tracking

Debate invocations produce multiple `TurnUsageRecord` entries per round:
- One record per participant per round
- `SubAgent` field uses a compound activity: `ideation_debate`, `triage_debate`
- Aggregate cost includes all rounds across all participants
- The debate's `Conclusion` includes total cost

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `WALLFACER_IDEATION_MODE` | `single` | Ideation mode: `single` (one agent) or `debate` (multi-agent) |
| `WALLFACER_TRIAGE_MODE` | `single` | Triage mode: `single` or `debate` |
| `WALLFACER_DEBATE_MAX_ROUNDS` | `3` | Maximum debate rounds (opening + rebuttals + closing) |
| `WALLFACER_DEBATE_TURN_ORDER` | `simultaneous` | Turn order: `simultaneous`, `round_robin`, `random` |
| `WALLFACER_DEBATE_PROVIDERS` | `claude,codex` | Comma-separated provider list for debates |
| `WALLFACER_DEBATE_MAX_COST` | `0` | Cost ceiling per debate in USD (0 = no limit) |
| `WALLFACER_DEBATE_ESCALATION` | `user` | Divergence escalation: `arbiter`, `user`, or `arbiter,user` |

### Per-Task Override

Tasks can override debate config via the API:

```json
{
  "debate": {
    "mode": "debate",
    "max_rounds": 4,
    "providers": ["claude", "codex", "gemini"],
    "turn_order": "round_robin"
  }
}
```

## UI

### Debate Transcript Panel

When a task involves a debate, the task detail view includes a "Debate" tab:

- **Timeline view:** Each round shown as a horizontal band. Within each band, participant responses shown as adjacent cards (like a chat with multiple speakers).
- **Participant colors:** Each provider gets a distinct color. Provider avatars (from pixel-agents) shown next to each response.
- **Convergence indicator:** A visual meter showing how much agreement exists. Updates after each round.
- **Final verdict:** Highlighted at the bottom — what was agreed, what was disputed, and how it was resolved.

### Ideation Debate View

For ideation debates, the transcript panel additionally shows:
- **Idea tracking:** Ideas that persist across rounds (endorsed by multiple agents) are visually connected with thread lines.
- **Kill/promote badges:** When an agent explicitly kills or promotes another's idea, the badge appears on the idea card.
- **Final ranked list:** The converged top-N ideas, with attribution (which agent originally proposed each).

### Signal Triage Debate View

For triage debates, the transcript panel shows:
- **Evidence citations:** Inline links to telemetry data points referenced by agents.
- **Diagnosis comparison:** Side-by-side diff of each agent's diagnosis.
- **Action recommendation:** The final triage action with a "Execute" button (or "Escalate" if diverged).

## API Changes

### Debate Management

| Method | Route | Description |
|--------|-------|-------------|
| `GET` | `/api/tasks/{id}/debate` | Get debate state and transcript |
| `GET` | `/api/tasks/{id}/debate/stream` | SSE: live debate round updates |

No explicit start/cancel endpoints — debates are started implicitly by the runner when ideation or triage mode is `debate`, and cancelled via the existing task cancel flow.

### Config

`GET /api/config` and `PUT /api/config` gain debate-related fields alongside the existing autopilot and consensus settings.

## Implementation Plan

### Phase 1 — Debate Engine

Core orchestrator logic, independent of any specific application.

1. Define `Debate`, `DebateRound`, `DebateResponse`, `DebateConfig` types in `internal/store/models.go`.
2. Add `DebateStatus` and `DebatePhase` enums.
3. Implement `runDebate()` in `internal/runner/debate.go`:
   - Round execution (simultaneous and round-robin turn orders)
   - Transcript construction (prompt building with prior rounds)
   - Cost tracking and budget enforcement
   - Early termination on convergence
4. Add debate prompt templates: `internal/prompts/debate.tmpl` (base), with `{{define "phase-opening"}}`, `{{define "phase-rebuttal"}}`, `{{define "phase-closing"}}` blocks.
5. Add convergence evaluation: lightweight headless agent that reads final positions and outputs structured agreement/disagreement.
6. Add debate event types to the event system.
7. Add debate env var parsing to `internal/envconfig/`.

**Depends on:** Agent abstraction Phase 1 (role descriptors for clean multi-provider invocation).

### Phase 2 — Ideation Debate

Wire the debate engine into the ideation flow.

1. Add `debate-ideation.tmpl` prompt template with workspace signals, existing tasks, rejected history.
2. Modify `runIdeationTask()` to dispatch to `runDebate()` when `WALLFACER_IDEATION_MODE=debate`.
3. Implement idea extraction from debate transcript — parse `IdeateResult[]` from each participant's closing response, then merge/rank.
4. Idea ranking: ideas endorsed by multiple participants rank highest; ideas challenged and not defended are dropped.
5. Output format remains `[]IdeateResult` — compatible with existing backlog-task-creation flow.
6. Add ideation debate UI (transcript panel with idea tracking).

**Depends on:** Phase 1.

### Phase 3 — Triage Debate

Wire the debate engine into the telemetry signal loop.

1. Add `debate-triage.tmpl` prompt template with anomaly data, telemetry context, correlated code.
2. Add `TriageDecision` and `TriageAction` types.
3. Integrate with telemetry auto-dispatch: when `WALLFACER_TRIAGE_MODE=debate`, anomaly detection triggers a debate before creating a fix task.
4. Implement triage action execution: hotfix (create task), rollback (trigger revert), monitor (schedule re-evaluation), ignore (dismiss), escalate (user waiting).
5. Add triage debate UI (diagnosis comparison, evidence citations).

**Depends on:** Phase 1, telemetry spec (signal detection and anomaly model).

### Phase 4 — Refinements

1. Add round-robin and random turn orders (Phase 1 starts with simultaneous only).
2. Add arbiter escalation for diverged debates (reuses arbiter from consensus spec).
3. Add debate history — track which debate patterns lead to better outcomes over time (feeds back into debate config tuning).
4. Cost analytics for debates vs single-agent approaches.

## Cost Analysis

Debate is more expensive than single-agent execution. Concrete estimates:

| Scenario | Providers | Rounds | Invocations | vs Single |
|----------|-----------|--------|-------------|-----------|
| Ideation debate (minimal) | 2 | 3 (open+rebuttal+close) | 6 | 6× |
| Ideation debate (full) | 3 | 4 | 12 | 12× |
| Triage debate (minimal) | 2 | 2 (open+close) | 4 | 4× |
| Triage debate (full) | 3 | 3 | 9 | 9× |

Mitigations:
- **Default is `single`.** Debate is opt-in for users who value deeper reasoning.
- **Early termination.** If agents converge after the opening round, skip rebuttals.
- **Cost ceiling.** `WALLFACER_DEBATE_MAX_COST` hard-stops the debate.
- **Headless agents for judging.** Convergence evaluation uses a lightweight single-turn agent (title-tier cost), not a full debate round.
- **Selective application.** Only high-stakes activities (ideation, triage) use debate. Low-stakes activities (title, commit message) use single or consensus.

## Relationship to Other Specs

| Spec | Relationship |
|------|-------------|
| **Agent Abstraction** | Debate uses `runAgent()` role descriptors to invoke participants. Each debate turn is a single-turn agent invocation with a different provider. |
| **Multi-Agent Consensus** | Consensus is for binary decisions (pass/fail). Debate is for open-ended analysis. A debate's closing round can feed into a consensus vote if needed. The arbiter and escalation mechanisms are shared. |
| **Telemetry & Observability** | Triage debate consumes anomaly data from the telemetry system. The debate conclusion drives the auto-dispatch action. |
| **Spec Coordination** | Future application: debate could be used for spec review — multiple agents critique a draft spec before it's accepted. Not in scope for this spec. |

## Open Questions

1. **Transcript context window.** Multi-round debates accumulate large transcripts. By round 3 with 3 participants, the prompt could exceed context limits. Should the orchestrator summarize prior rounds (lossy) or truncate older rounds (recency bias)?

2. **Role assignment.** Should participants be assigned explicit roles (proposer, devil's advocate, synthesizer) or let them emerge naturally? Fixed roles create predictable structure; fluid roles may produce richer interaction but risk participants talking past each other.

3. **Provider capability mismatch.** Claude, Codex, and Gemini have different strengths. Should the debate framework account for this (e.g., weight votes by provider capability for the specific task domain)? Or treat all participants equally and let the debate itself surface quality differences?

4. **Debate for code review.** A natural extension: Agent A writes code, Agent B reviews it, Agent A responds to review comments, iterating until both agree. This is debate applied to the implementation activity itself. Is this in scope, or does it belong in the consensus spec's "deferred work"?

5. **Learning from debates.** Over many debates, patterns emerge (e.g., Codex consistently catches performance issues Claude misses). Should the system learn these patterns and adjust future debate configurations? This is a reinforcement learning problem and likely out of scope for v1.

6. **Abort on strong consensus.** If all agents agree in the opening round, should the debate skip directly to conclusion? This saves cost but might miss nuances that rebuttals would surface. Configurable threshold?

## Deferred Work

- **Debate for code review / implementation.** Iterative review cycles between implementation agent and review agent. Architecturally similar but requires shared write access to worktrees (fundamentally different from read-only debate).
- **Debate visualization with graph view.** Map idea evolution across rounds as a directed graph — which ideas were combined, split, or killed. Rich UI feature, not needed for v1.
- **Adaptive debate length.** Dynamically adjust max rounds based on observed convergence rate rather than a fixed ceiling.
- **Provider-weighted voting.** Weight final votes by provider track record for the specific task domain.
- **Debate templates.** User-defined debate structures (custom roles, custom phase instructions, domain-specific evaluation criteria) — a debate DSL.
