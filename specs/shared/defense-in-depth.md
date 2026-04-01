---
title: "Defense in Depth — Layered Oversight for Task Orchestration"
status: drafted
depends_on:
  - specs/shared/agent-abstraction.md
  - specs/shared/sandbox-hooks.md
  - specs/shared/telemetry-observability.md
affects:
  - internal/runner/
  - internal/handler/
  - internal/store/
  - internal/inbox/
effort: xlarge
created: 2026-04-01
updated: 2026-04-01
author: changkun
dispatched_task_id: null
---

# Defense in Depth — Layered Oversight for Task Orchestration

---

## Problem

Wallfacer dispatches AI agents into containers to write code autonomously. Multiple specs address pieces of the oversight puzzle — risk scoring detects dangerous actions, sandbox hooks intercept tool calls, multi-agent consensus cross-verifies results, telemetry observes runtime behavior. But these specs were designed independently. There is no design that governs how they **compose** into a defense stack, and there are structural gaps that no existing spec covers.

Three specific problems:

1. **No composition constraint.** The Swiss cheese model from risk management requires that adjacent layers have **orthogonal weaknesses** — if one layer fails, the next catches it precisely because it fails differently. Wallfacer's oversight specs don't express this relationship. A risk scoring false negative and a consensus verification gap could align, and nothing in the design prevents it.

2. **No task-level permission model.** Claude Code inside the container has its own permission system (plan/auto/default modes, deny/allow rules). But Wallfacer itself has no equivalent at the orchestration level. There is no way to say "tasks in this workspace can only modify `src/`" or "auto-dispatched tasks require human confirmation before touching database schemas." The container is either launched or it isn't.

3. **No pre-dispatch validation.** A task prompt goes straight from the UI to a container with no pre-flight check. There is no prompt injection detection, no scope validation ("does this prompt ask for something outside the workspace?"), no resource estimation ("will this blow the cost budget before producing useful output?"). Claude Code validates tool inputs (schema + semantic checks) before executing them — Wallfacer should validate task inputs before dispatching them.

---

## Goals

1. **Define the layer stack.** Enumerate every oversight mechanism (existing, specified, and new) and assign it a position in a defense-in-depth stack with explicit orthogonality analysis.
2. **Add task-level permission modes.** Let users set coarse trust postures per workspace or per task that constrain what agents can attempt.
3. **Add pre-dispatch validation.** Check task prompts before container launch for scope violations, injection patterns, and budget feasibility.
4. **Add decision audit unification.** Consolidate oversight decisions from all layers into a single queryable log per task.
5. **Define escalation cascade.** When a layer blocks an action, specify how the decision propagates — retry, escalate to human, abort task, or quarantine for review.

---

## Non-Goals (v1)

- Replace Claude Code's internal permission system (that remains the in-container defense).
- Formal verification of layer orthogonality (the analysis is design-time reasoning, not runtime proof).
- ML-based prompt injection detection (v1 uses pattern matching).
- Per-file ACLs with filesystem enforcement (permission modes are advisory constraints communicated to the agent via prompt, not OS-level enforcement).

---

## Design

### The Layer Stack

Wallfacer's defense-in-depth stack has 10 layers, ordered from outermost (earliest interception) to innermost (last resort). Each layer is tagged with its implementation status and the spec that owns it.

```
     Task prompt from user or auto-dispatch
        |
        v
+-------------------------------------------+
| L1  Pre-Dispatch Validation               |  NEW — this spec
|     Prompt scope, injection, budget check  |
+-------------------------------------------+
| L2  Permission Mode Gate                   |  NEW — this spec
|     Workspace/task trust posture           |
+-------------------------------------------+
| L3  Refinement Checkpoint                  |  EXISTING — runner/refine
|     Agent-assisted prompt improvement      |
+-------------------------------------------+
| L4  System Prompt & Instructions           |  EXISTING — prompts/, AGENTS.md
|     Role constraints, workspace rules      |
+-------------------------------------------+
| L5  Container Sandbox                      |  EXISTING — sandbox backends
|     OS-level isolation (podman/docker)     |
+-------------------------------------------+
| L6  Sandbox Hooks                          |  SPEC — sandbox-hooks.md
|     Tool-call interception, command guard  |
+-------------------------------------------+
| L7  In-Container Defenses                  |  INHERITED — Claude Code / Codex
|     CC's own 14-layer Swiss cheese model   |
+-------------------------------------------+
| L8  Risk Scoring                           |  SPEC — oversight-risk-scoring.md
|     Real-time action risk assessment       |
+-------------------------------------------+
| L9  Multi-Agent Verification               |  SPEC — multi-agent-consensus.md
|     Cross-provider result validation       |
+-------------------------------------------+
| L10 Human Review & Escalation              |  EXISTING + NEW
|     Waiting loop, denial limits, audit     |
+-------------------------------------------+
        |
        v
     Task result (done / waiting / failed)
```

### Orthogonality Analysis

The value of defense in depth comes from layers failing **independently**. If two adjacent layers share the same weakness, they collapse into one effective layer.

| Layer Pair | L(n) Weakness | L(n+1) Compensates Because |
|------------|---------------|---------------------------|
| L1→L2 | Pattern-based injection detection has false negatives | Permission mode constrains blast radius regardless of prompt content |
| L2→L3 | Permission modes are static; can't anticipate novel risks | Refinement agent can flag underspecified or risky prompts dynamically |
| L3→L4 | Refinement is optional and model-dependent | System prompt is deterministic and always present |
| L4→L5 | System prompt is advisory; agent can ignore it | Container sandbox is OS-enforced; agent cannot escape it |
| L5→L6 | Container allows all operations within its mount | Hooks intercept specific dangerous tool calls before execution |
| L6→L7 | Hooks only cover Wallfacer-defined patterns | CC's internal scanner covers 23 injection categories + AI classifier |
| L7→L8 | CC's defenses optimize for safety within a session | Risk scoring evaluates cumulative risk across the full task lifecycle |
| L8→L9 | Risk scoring is single-model, single-perspective | Multi-agent consensus uses different providers to eliminate single-model bias |
| L9→L10 | Consensus can fail when all providers share the same blind spot | Human review catches what no automated system anticipates |

Key orthogonality properties:
- **L1 is pattern-based, L2 is policy-based** — different failure modes.
- **L4 is advisory, L5 is enforced** — the strongest boundary in the stack.
- **L7 is intra-session, L8 is cross-session** — different temporal scope.
- **L9 is multi-model, L10 is human** — different cognitive architecture.

---

### L1: Pre-Dispatch Validation (New)

Before a task enters a container, validate the prompt against three dimensions:

#### Scope Validation

Check that the prompt's intent aligns with the workspace's purpose. This is lightweight — not a full semantic analysis, but a structural check:

- **Path references**: If the prompt mentions files outside the workspace paths, flag it. (Regex extraction of path-like strings, compared against configured workspace roots.)
- **Operation scope**: If the prompt asks for operations on external systems (deploy to production, push to remote, send emails) and the task's permission mode doesn't allow external effects, block or flag.
- **Workspace mismatch**: If the workspace has an AGENTS.md that restricts scope (e.g., "this workspace is for frontend only"), check for obvious contradictions.

#### Injection Detection

Pattern-based detection of prompt injection attempts in task prompts. This matters for auto-dispatched tasks (from telemetry anomalies, inbox items, or batch creation) where the prompt may include external content:

- **Instruction override patterns**: "Ignore previous instructions", "You are now", "System:", common injection prefixes.
- **Encoded payloads**: Base64-encoded instructions, Unicode homoglyph substitution, zero-width characters.
- **Context manipulation**: Fake tool results, fabricated conversation history, role-play prompts.

Injection detection is conservative — it flags for human review rather than blocking outright. False positives are preferable to false negatives when the prompt source is automated.

#### Budget Feasibility

Before launching a container, estimate whether the task is likely to complete within its budget:

- **Token estimate**: Based on prompt length, goal complexity heuristic (word count, number of files referenced), and historical task completion data for similar prompts.
- **Cost estimate**: Token estimate * model price per token.
- **Budget check**: If estimated cost exceeds the task's `MaxCostUSD` or `MaxInputTokens`, warn the user before dispatch.

Budget estimation is inherently approximate. The goal is catching obvious mismatches (a task that will clearly blow its budget) rather than precise prediction.

```go
// PreDispatchResult captures the outcome of pre-dispatch validation.
type PreDispatchResult struct {
    Allowed       bool                `json:"allowed"`
    Warnings      []ValidationWarning `json:"warnings,omitempty"`
    Blocked       bool                `json:"blocked"`
    BlockReason   string              `json:"block_reason,omitempty"`
    BudgetEstimate *BudgetEstimate    `json:"budget_estimate,omitempty"`
}

type ValidationWarning struct {
    Kind    string `json:"kind"`    // "scope", "injection", "budget"
    Message string `json:"message"`
    Detail  string `json:"detail,omitempty"`
}

type BudgetEstimate struct {
    EstimatedTokens int64   `json:"estimated_tokens"`
    EstimatedCostUSD float64 `json:"estimated_cost_usd"`
    TaskBudgetUSD   float64  `json:"task_budget_usd"`
    Feasible        bool    `json:"feasible"`
}
```

---

### L2: Permission Mode Gate (New)

Analogous to Claude Code's permission modes but operating at the task/workspace level.

#### Modes

| Mode | Behavior | Use Case |
|------|----------|----------|
| `open` | No additional constraints beyond container sandbox | Trusted workspace, experienced user |
| `guarded` | Agent receives scope constraints in prompt; risk scoring thresholds lowered | Default for most workspaces |
| `restricted` | File modification limited to declared paths; external effects blocked; human confirmation required for high-risk actions | Sensitive codebases, production-adjacent repos |
| `review` | Every task pauses at waiting after first turn for human review before continuing | Onboarding, audit-required workflows |
| `readonly` | Agent can read and analyze but cannot write files or run mutating commands | Code review, exploration, learning |

#### Scope Constraints

In `guarded` and `restricted` modes, the user can declare scope constraints:

```yaml
# Per-workspace permission config (~/.wallfacer/permissions/<fingerprint>.yaml)
mode: guarded
constraints:
  allowed_paths:
    - src/
    - tests/
    - docs/
  denied_paths:
    - .env
    - internal/store/migrations/
    - deploy/
  allowed_operations:
    - file_edit
    - file_create
    - test_run
    - build
  denied_operations:
    - git_push
    - deploy
    - external_api_call
  max_cost_usd: 5.00
  require_human_review_for:
    - database_schema_changes
    - dependency_additions
    - security_sensitive_files
```

**Enforcement mechanism**: Permission modes are communicated to the agent through **prompt injection** (added to the system prompt) and **hook enforcement** (sandbox hooks block operations that violate constraints). This is defense in depth within the layer itself:
- The prompt tells the agent what it shouldn't do (advisory).
- The hook blocks it if it tries anyway (enforced).
- The risk scorer flags it if the hook misses it (detective).

#### Per-Task Override

Individual tasks can override the workspace mode:

```go
type Task struct {
    // ... existing fields ...
    PermissionMode  string   `json:"permission_mode,omitempty"`  // overrides workspace mode
    AllowedPaths    []string `json:"allowed_paths,omitempty"`    // overrides workspace constraints
    DeniedPaths     []string `json:"denied_paths,omitempty"`
}
```

Auto-dispatched tasks (from telemetry anomalies or inbox items) default to `restricted` mode regardless of workspace setting. The user must explicitly promote them to a less restrictive mode.

---

### L10: Human Review & Escalation (Extended)

The existing waiting/feedback loop is one mechanism. This layer adds two more:

#### Denial Tracking

Track how often each layer blocks or flags actions within a task:

```go
type DenialRecord struct {
    Timestamp time.Time     `json:"timestamp"`
    Layer     int           `json:"layer"`       // 1-10
    LayerName string        `json:"layer_name"`
    Action    string        `json:"action"`      // what was attempted
    Reason    string        `json:"reason"`      // why it was blocked
    Outcome   string        `json:"outcome"`     // "blocked", "flagged", "escalated"
}
```

Escalation triggers:
- **3 consecutive blocks from the same layer** → pause task, notify user.
- **Blocks from 3+ different layers on the same action** → the action is likely genuinely dangerous; abort and quarantine.
- **10 total blocks in a single task** → pause for human review regardless of layer distribution.

#### Decision Audit Log

Every oversight decision (from any layer) writes to a unified per-task audit log:

```go
type OversightDecision struct {
    ID        uuid.UUID     `json:"id"`
    TaskID    uuid.UUID     `json:"task_id"`
    Timestamp time.Time     `json:"timestamp"`
    Layer     int           `json:"layer"`
    LayerName string        `json:"layer_name"`
    Decision  string        `json:"decision"`   // "allow", "block", "flag", "escalate"
    Reason    string        `json:"reason"`
    Input     string        `json:"input"`       // truncated action description
    Tags      []string      `json:"tags,omitempty"`
}
```

The audit log is stored alongside the task's event timeline (existing `events.jsonl`) as `oversight.jsonl`. It is queryable via API and surfaced in the task detail UI.

---

### Escalation Cascade

When any layer produces a non-allow decision, the cascade determines what happens next:

```
Layer produces "block" or "flag"
    │
    ├─► "block" ──► Is task in auto-dispatch mode?
    │                 ├─► Yes: pause task → waiting (human must review)
    │                 └─► No:  feed block reason to agent as error
    │                          Agent can retry with different approach
    │                          └─► 3 retries exhausted? → pause task
    │
    └─► "flag" ───► Log to audit trail
                    ├─► Risk score > 0.8? → treat as "block"
                    └─► Risk score ≤ 0.8? → continue, show badge in UI
```

The cascade ensures that:
- **Auto-dispatched tasks are never fully autonomous** when oversight layers fire — they always pause for human review.
- **User-dispatched tasks get agent-level error feedback** first, allowing the agent to self-correct before human escalation.
- **Flags accumulate visibly** in the UI so the user can intervene proactively.

---

## API

### Pre-Dispatch Validation

| Method | Route | Description |
|--------|-------|-------------|
| `POST` | `/api/tasks/validate` | Validate a task prompt without creating the task |

### Permission Modes

| Method | Route | Description |
|--------|-------|-------------|
| `GET` | `/api/permissions` | Get workspace permission mode and constraints |
| `PUT` | `/api/permissions` | Update workspace permission config |
| `GET` | `/api/permissions/modes` | List available modes with descriptions |

### Oversight Audit

| Method | Route | Description |
|--------|-------|-------------|
| `GET` | `/api/tasks/{id}/oversight/decisions` | Query oversight decision log for a task |
| `GET` | `/api/tasks/{id}/oversight/denials` | Summary of denial records for a task |

---

## UI

### Permission Mode Indicator

The toolbar shows the current workspace permission mode as a small badge (color-coded: green=open, yellow=guarded, orange=restricted, blue=review, gray=readonly). Clicking opens the permission configuration panel.

### Pre-Dispatch Warnings

When creating a task, if pre-dispatch validation produces warnings:
- A yellow warning banner appears below the prompt editor.
- Each warning is expandable (click to see detail).
- The user can proceed despite warnings or edit the prompt.
- Blocked tasks show a red banner with the block reason; the "Create" button is disabled until the issue is resolved.

### Oversight Timeline

The task detail view gains an "Oversight" tab showing:
- A vertical timeline of all oversight decisions across all layers.
- Color-coded by decision type (green=allow, yellow=flag, red=block, orange=escalate).
- Each entry shows the layer, reason, and the action that triggered it.
- Filterable by layer and decision type.

### Defense Dashboard

A new view (accessible from Settings or a toolbar icon) showing:
- Which layers are active (some depend on unimplemented specs).
- Per-layer statistics: how many decisions, block rate, false positive rate (user-overridden blocks).
- Overall defense health: are any layers disabled or degraded?

---

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `WALLFACER_PERMISSION_MODE` | `guarded` | Default workspace permission mode |
| `WALLFACER_PREDISPATCH_ENABLED` | `true` | Enable pre-dispatch validation |
| `WALLFACER_PREDISPATCH_INJECTION_CHECK` | `true` | Enable prompt injection pattern detection |
| `WALLFACER_DENIAL_CONSECUTIVE_LIMIT` | `3` | Consecutive blocks before task pause |
| `WALLFACER_DENIAL_TOTAL_LIMIT` | `10` | Total blocks before forced human review |
| `WALLFACER_AUTO_DISPATCH_MODE` | `restricted` | Permission mode forced on auto-dispatched tasks |

---

## Implementation Phases

### Phase 1 — Pre-dispatch validation

| File | Change |
|------|--------|
| `internal/oversight/` (new package) | `PreDispatchValidator`, scope/injection/budget checks |
| `internal/oversight/patterns.go` | Injection detection patterns |
| `internal/handler/tasks.go` | Call validator before task creation; add `POST /api/tasks/validate` |

**Depends on:** Nothing. Can start immediately.
**Effort:** Medium.

### Phase 2 — Permission modes

| File | Change |
|------|--------|
| `internal/oversight/permissions.go` | Permission mode definitions, constraint evaluation |
| `internal/oversight/permissions_config.go` | YAML config loading from `~/.wallfacer/permissions/` |
| `internal/runner/` | Inject permission constraints into agent system prompt |
| `internal/handler/permissions.go` | Permission CRUD endpoints |
| `ui/js/permissions.js` | Permission config panel, mode indicator badge |

**Depends on:** Phase 1.
**Effort:** Medium.

### Phase 3 — Decision audit log

| File | Change |
|------|--------|
| `internal/oversight/audit.go` | `OversightDecision` type, audit log writer/reader |
| `internal/store/` | `oversight.jsonl` per-task persistence |
| `internal/handler/oversight.go` | Decision query endpoints |
| `ui/js/oversight.js` | Oversight timeline in task detail |

**Depends on:** Phase 1.
**Effort:** Medium.

### Phase 4 — Denial tracking & escalation cascade

| File | Change |
|------|--------|
| `internal/oversight/denial.go` | Denial counter, escalation trigger logic |
| `internal/runner/` | Integrate escalation cascade into task execution loop |

**Depends on:** Phase 3 (audit log for recording denials).
**Effort:** Low.

### Phase 5 — Hook integration

| File | Change |
|------|--------|
| `internal/oversight/hooks.go` | Bridge between sandbox hooks and permission mode constraints |
| `internal/runner/` | Enforce denied operations via hook responses |

**Depends on:** Phase 2, sandbox-hooks.md implementation.
**Effort:** Medium.

### Phase 6 — Defense dashboard

| File | Change |
|------|--------|
| `internal/handler/oversight.go` | Layer status and statistics endpoints |
| `ui/js/defense.js` | Defense dashboard view |

**Depends on:** Phase 3 (audit data to display).
**Effort:** Low.

---

## Relationship to Other Specs

This spec is a **composition layer** — it does not replace existing oversight specs but defines how they work together.

| Spec | Role in Stack | This Spec Adds |
|------|--------------|----------------|
| [sandbox-hooks.md](sandbox-hooks.md) | L6: Tool-call interception | Hook responses informed by permission mode constraints |
| [oversight-risk-scoring.md](../local/oversight-risk-scoring.md) | L8: Action risk assessment | Risk scores feed into escalation cascade |
| [multi-agent-consensus.md](multi-agent-consensus.md) | L9: Cross-provider verification | Consensus failures trigger escalation |
| [telemetry-observability.md](telemetry-observability.md) | Runtime anomaly detection | Auto-dispatched fix tasks enter at L1 with `restricted` mode |
| [intelligence-system.md](intelligence-system.md) | Cross-task awareness | World model informs scope validation in L1 |
| [information-inbox.md](information-inbox.md) | External signal ingestion | Inbox-converted tasks enter at L1 with validation |

---

## Inspiration

The Swiss cheese model is borrowed from [Claude Code's oversight architecture](https://github.com/anthropics/claude-code/blob/main/docs/architecture/oversight-design.md), which implements 14 layers of defense for tool-call safety within a single agent session. This spec adapts the same philosophy to the **task orchestration** level, where the unit of defense is a task (not a tool call) and the boundary is a container (not a process sandbox).

Key adaptations:
- **Claude Code's L2 (input validation) → Wallfacer's L1 (pre-dispatch validation).** CC validates tool inputs; Wallfacer validates task prompts.
- **Claude Code's L3 (permission rules) → Wallfacer's L2 (permission modes).** CC has per-tool deny/allow rules; Wallfacer has per-workspace/task trust postures.
- **Claude Code's L10 (AI classifier) → Wallfacer's L9 (multi-agent consensus).** CC uses a 2-stage LLM to replace human approval; Wallfacer uses cross-provider verification to validate results.
- **Claude Code's L13 (audit log) → Wallfacer's L10 (unified decision audit).** Both log all decisions for post-hoc analysis.

The in-container defenses (CC's layers 1-14) remain fully active as Wallfacer's L7. The two systems compose: Wallfacer's stack wraps around CC's stack.

---

## Potential Challenges

1. **Permission mode UX complexity.** Five modes with per-workspace constraints could overwhelm users. The default (`guarded`) should work well without configuration. The UI must make the current mode visible without requiring users to understand the full model.

2. **Prompt injection arms race.** Pattern-based detection catches known patterns but not novel attacks. The conservative approach (flag, don't block) limits damage from false negatives, but auto-dispatched tasks from external sources (inbox, telemetry) are the highest-risk vector.

3. **Advisory vs enforced constraints.** Permission mode constraints in the prompt are advisory — the agent may ignore them. Hook-based enforcement covers some operations but not all (e.g., the agent can write arbitrary content to allowed files). The gap between advisory and enforced is acknowledged, not solved.

4. **Audit log volume.** High-frequency tasks could generate large oversight logs. Retention and pruning policies (aligned with existing event timeline retention) are needed.

5. **Layer dependency chain.** Some layers depend on specs that aren't implemented yet (hooks, risk scoring, consensus). The stack degrades gracefully — missing layers are simply absent, and the remaining layers still provide defense. The defense dashboard makes missing layers visible.

---

## Open Questions

1. **Should permission modes be per-workspace or per-workspace-group?** Workspace groups can contain multiple repos with different sensitivity levels. Per-workspace is more granular but more configuration burden.

2. **Prompt injection detection for non-English prompts?** The pattern database will primarily cover English injection patterns. Multi-language support adds significant complexity. Should v1 be English-only with a clear extension point?

3. **How does the escalation cascade interact with auto-retry?** If a task is paused by the cascade, should auto-retry respect the pause or bypass it? The safe default is respect (auto-retry only fires on failures, not oversight pauses), but this needs explicit specification.

4. **Defense dashboard as ops tool vs user feature?** Should the dashboard be visible to all users or only in a "developer mode"? Showing layer statistics could confuse non-technical users but is valuable for understanding system behavior.

5. **Audit log format compatibility with OpenTelemetry?** The telemetry spec uses OTLP. Should oversight decisions also emit as OTLP spans/events for unified observability in external tools (Grafana, Datadog)?
