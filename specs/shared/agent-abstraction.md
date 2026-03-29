# Agent Role Abstraction & Multi-Agent Communication

**Status:** Investigation / Design
**Dependencies:** [Sandbox Backends](../foundations/sandbox-backends.md), [Container Reuse](../foundations/container-reuse.md)
**Scope:** `internal/runner/`, `internal/store/`, `internal/prompts/`

## Problem Statement

The runner package hard-codes seven agent roles (implementation, testing, refinement, title, oversight, commit_message, idea_agent) with significant structural duplication. Adding a new role requires touching 6+ files and duplicating container-launch, usage-tracking, output-parsing, and sandbox-fallback logic. There is no mechanism for agents to communicate with each other during execution beyond the static board manifest.

### Current Pain Points

1. **Role explosion cost.** Each new agent role requires:
   - A `SandboxActivity` constant in `store/models.go`
   - An env var `WALLFACER_SANDBOX_<ROLE>` in `envconfig/`
   - A dedicated execution function (`title.go`, `refine.go`, `ideate.go`, etc.)
   - A container spec builder (often a copy-paste variant of `buildBaseContainerSpec`)
   - A container registry slot
   - Sandbox-fallback logic (identical across all roles)
   - Usage accumulation boilerplate

2. **Duplicated container lifecycle.** Four nearly identical container-launch sequences exist:
   - `runContainer()` in `execute.go` (implementation/test)
   - `runRefinement()` in `refine.go`
   - `RunIdeation()` in `ideate.go`
   - `generateTitle()` / `generateOversight()` in `title.go` / `oversight.go`

   Each repeats: build spec → register container → launch → read stdout/stderr → wait → parse NDJSON → accumulate usage → handle fallback.

3. **No inter-agent communication.** Agents can only observe each other's results after completion, via the static board manifest (`board.json`). There is no way for two running agents to exchange messages, coordinate, or pipeline results.

4. **Rigid prompt construction.** Prompt templates (`internal/prompts/*.tmpl`) are rendered server-side and injected as the `-p` flag. There is no standard way for an agent to declare its input schema, output schema, or contract with the orchestrator.

## Design Goals

- **G1 — Low-friction role extension.** Adding a new agent role should require defining a role descriptor (name, prompt template, permissions, output parser) and nothing else. No new files, no duplicated container logic.
- **G2 — Composable agents.** Support agent pipelines (A's output feeds B's input) and agent groups (A and B run concurrently, sharing a communication channel).
- **G3 — Backward compatibility.** Existing seven roles must work identically. No API changes to handlers. No migration for users.
- **G4 — Sandbox agnostic.** The abstraction must not assume a specific sandbox backend (local, K8s, native).

## Design Space

### Option A — Role Descriptor Registry

Extract the repeated container-launch, usage-tracking, and fallback logic into a single generic `runAgent()` method. Each agent role becomes a declarative descriptor:

```go
type AgentRole struct {
    Activity      store.SandboxActivity
    PromptTmpl    string                 // template name in internal/prompts/
    Timeout       time.Duration
    ReadOnly      bool                   // workspace mount mode
    MountBoard    bool                   // include board.json
    MountSiblings bool                   // include sibling worktrees
    OutputParser  func(agentOutput) (any, error)
    Registry      *containerRegistry     // which registry to track in
    SingleTurn    bool                   // no --resume loop
}
```

A central `runAgent(ctx, role AgentRole, task, prompt, opts)` handles:
- Sandbox selection (existing 4-tier hierarchy)
- Container spec building (dispatching on `role.ReadOnly`, `role.MountBoard`, etc.)
- Container lifecycle (register → launch → read → wait → parse)
- Usage accumulation
- Sandbox fallback on token-limit errors

**Pros:**
- Minimal refactor — extracts existing code, doesn't redesign
- Adding a role = one `AgentRole` var + a prompt template
- Zero risk to existing behavior (same code paths, just unified)

**Cons:**
- Does not address inter-agent communication (G2)
- Role descriptor may grow unwieldy if roles diverge significantly
- Still couples all roles to the same container-launch code path

### Option B — Agent Interface + Pluggable Executors

Define an `Agent` interface that encapsulates the full lifecycle:

```go
type Agent interface {
    Activity() store.SandboxActivity
    BuildSpec(ctx, task, prompt) (sandbox.ContainerSpec, error)
    ParseOutput(agentOutput) (AgentResult, error)
    OnComplete(ctx, task, result AgentResult) error
}

type AgentResult struct {
    Structured any              // role-specific parsed output
    Usage      store.TaskUsage
    Session    string           // for resumable agents
    StopReason string
}
```

A generic executor (`AgentExecutor`) handles container lifecycle, usage tracking, fallback, and registry management. Each role implements `Agent`:

```go
type TitleAgent struct{ ... }
type OversightAgent struct{ ... }
type RefinementAgent struct{ ... }
// ... new roles just implement the interface
```

**Pros:**
- Clean separation of concerns — roles own their spec + parsing, executor owns lifecycle
- Easier to test (mock the Agent interface)
- Roles can override any aspect without touching shared code

**Cons:**
- Larger refactor surface
- Interface may be too rigid for roles that need fundamentally different execution models (multi-turn vs single-turn, stateful session vs stateless)
- Still doesn't solve inter-agent communication

### Option C — Agent Graph (Pipelines & Groups)

Build on Option A or B, but add a coordination layer that can compose agents:

```go
type AgentGraph struct {
    Nodes []AgentNode
    Edges []AgentEdge // data flow: A.output → B.input
}

type AgentNode struct {
    Role     AgentRole  // or Agent interface
    InputFn  func(priorResults map[string]AgentResult) string // build prompt from predecessors
}

type AgentEdge struct {
    From, To string    // node names
    Type     EdgeType  // Sequential, Parallel, Conditional
}
```

Execution model:
1. **Sequential pipeline:** A finishes → result piped into B's prompt → B runs. Example: refine → implement → test → oversight.
2. **Parallel group:** A and B run concurrently, results collected when all finish. Example: oversight + title generation after task completion.
3. **Conditional edge:** B only runs if A's result meets a predicate. Example: auto-retry only if failure is retryable.

This is how the existing task execution already works informally (implement → commit → title/oversight in parallel), but it's hard-coded in `execute.go`.

**Pros:**
- Makes the implicit execution graph explicit and extensible
- Users could define custom agent pipelines via config
- Natural fit for "multi-agent workflows" (G2)

**Cons:**
- Significant complexity increase
- Graph scheduling, error propagation, and cancellation are hard problems
- May be over-engineered for the current set of roles

### Option D — Shared Communication Channel (Message Bus)

Add a communication mechanism for concurrently running agents. Two sub-options:

#### D1 — File-Based Message Passing

Mount a shared directory (`/workspace/.messages/`) into all containers belonging to the same task or group. Agents write NDJSON messages; the orchestrator (or other agents) poll for new files.

```
/workspace/.messages/
  agent-a/outbox/001.json   # agent A writes here
  agent-b/outbox/001.json   # agent B writes here
  shared/board.json          # orchestrator-maintained shared state
```

**Pros:** Simple, works with any sandbox backend, no new protocol.
**Cons:** Polling latency, no real-time signaling, agents must know the convention.

#### D2 — Orchestrator-Mediated Messaging

The orchestrator (runner) reads agent stdout in real-time (already done for NDJSON parsing). Agents emit structured messages (e.g., `{"type":"message","to":"agent-b","payload":...}`). The orchestrator routes messages by injecting them into the target agent's stdin or by writing them to the shared mount.

```
Agent A ──stdout──▶ Orchestrator ──mount/stdin──▶ Agent B
Agent B ──stdout──▶ Orchestrator ──mount/stdin──▶ Agent A
```

**Pros:** Real-time, orchestrator has full visibility and control.
**Cons:** Requires stdin injection support in sandbox backends; more complex than file-based; Claude Code doesn't natively support receiving messages on stdin during `-p` execution.

#### D3 — Tool-Based Communication (MCP)

Register an MCP server that agents can call to send/receive messages. The MCP server is hosted by the orchestrator and exposed to containers via a mounted socket or network endpoint.

```
Agent A ──MCP call──▶ Orchestrator MCP Server ──MCP push──▶ Agent B
```

**Pros:** Native integration with Claude Code's tool-use model; structured; bidirectional.
**Cons:** Requires MCP server infrastructure; Claude Code MCP support is still evolving; adds a network dependency.

### Option E — Hybrid (Recommended Investigation Path)

Combine Options A + C + D1 as a layered approach:

1. **Layer 1 — Role Descriptors (Option A):** Immediate refactor. Unify the container-launch path. Zero behavior change. Unlocks easy role addition.

2. **Layer 2 — Agent Graph (Option C, simplified):** Model the existing implicit pipeline as an explicit graph. Start with `Sequential` and `Parallel` edge types only. No user-configurable graphs yet — just make the code reflect the actual execution flow.

3. **Layer 3 — File-Based Communication (Option D1):** For multi-agent scenarios, mount a shared message directory. Define a simple message schema. Orchestrator writes initial context; agents can write messages that the orchestrator forwards.

Each layer is independently valuable and can be shipped incrementally.

## Analysis of Multi-Agent Use Cases

Before committing to a design, enumerate concrete use cases for multi-agent communication:

### Use Case 1: Parallel Implementation with Coordination

Two agents work on related parts of the same codebase. Agent A implements feature X; agent B implements feature Y. They need to avoid merge conflicts and may need to coordinate on shared interfaces.

**Communication needs:** Shared file awareness (which files each agent is editing), periodic sync points, ability to read each other's in-progress work.

**Current support:** Sibling worktree mounts provide read-only access to completed work. No support for in-progress coordination.

### Use Case 2: Review Agent

A dedicated review agent reads another agent's completed work and provides feedback, which the implementation agent then addresses.

**Communication needs:** Sequential pipeline (implement → review → revise). Review output must be structured and actionable.

**Current support:** Could be modeled as feedback on a waiting task, but requires manual intervention.

### Use Case 3: Architect + Implementer

An architect agent decomposes a high-level goal into sub-tasks with specifications. Multiple implementer agents execute the sub-tasks. The architect monitors progress and adjusts.

**Communication needs:** Dynamic task creation, progress monitoring, specification documents passed between agents.

**Current support:** Ideation agent + board manifest partially covers this. Epic coordination spec addresses the planning side.

### Use Case 4: Test-Driven Pair

A test agent writes tests first; an implementation agent makes them pass. They alternate in a red-green cycle.

**Communication needs:** Turn-based alternation, shared worktree with write access for both agents (serialized), test results passed as structured data.

**Current support:** The existing implementation→test flow is sequential but one-shot, not iterative.

### Use Case 5: Specialist Consultation

During implementation, an agent encounters a domain it's unfamiliar with and spawns a specialist agent (e.g., security review, performance analysis, database schema design) to provide guidance.

**Communication needs:** On-demand agent spawning, structured query/response, results injected into the calling agent's context.

**Current support:** None. Agents cannot spawn sub-agents.

## Evaluation Criteria

| Criterion | Weight | Notes |
|-----------|--------|-------|
| Implementation effort | High | Must be achievable incrementally |
| Backward compatibility | High | Existing roles must not break |
| Role addition friction | High | Primary pain point |
| Multi-agent expressiveness | Medium | Important but not all use cases are equally likely |
| Testability | Medium | Must be unit-testable without real containers |
| Sandbox backend independence | Medium | Must work with local + future K8s backends |
| User configurability | Low | Power-user feature, not needed in v1 |

## Open Questions

1. **How much role divergence is real vs. accidental?** Are the differences between refine/ideate/oversight containers fundamental (different security models) or incidental (copy-paste that drifted)? If incidental, Option A is sufficient. If fundamental, Option B is needed.

2. **Is multi-agent communication a near-term need?** If the primary pain is role extension friction (adding agent #8, #9, #10), Option A alone solves it. Multi-agent communication (Options C/D) can wait.

3. **What agent roles are planned?** Concrete near-term roles would clarify whether the abstraction needs to support read-write agents, stateful sessions, or only single-turn read-only agents.

4. **Should agent graphs be user-definable?** If yes, this implies a configuration format (YAML/JSON workflow definitions), validation, and a UI for building pipelines. This is a much larger scope.

5. **How does this interact with container reuse?** Container reuse introduces long-lived aux worker containers. The agent abstraction should be compatible — a reusable container is just a persistent `sandbox.Handle` that multiple agent invocations share.

6. **MCP as the communication protocol?** Claude Code already supports MCP tools. If the orchestrator exposed an MCP server, agents could use tool calls to communicate. This is architecturally clean but adds infrastructure complexity.

## Audit Results: Role Divergence Classification

The audit compared all seven roles across container spec building, launch sequence, output parsing, usage tracking, sandbox selection, and error handling.

### Role Tier Clustering

The 7 roles cluster into 3 tiers based on their fundamental container needs:

| Tier | Roles | Workspace Mounts | Multi-turn | Session Recovery |
|------|-------|-------------------|------------|------------------|
| **Heavyweight** | Implementation, Testing | RW + board + siblings | Yes | Yes (impl only) |
| **Inspector** | Refinement, Ideation | RO, no board | No | No |
| **Headless** | Title, Oversight, Commit Msg | None | No | No |

### Fundamental Differences (4 boolean dimensions)

These reflect genuinely different security/execution models:

| Aspect | Heavyweight | Inspector | Headless |
|--------|-------------|-----------|----------|
| Workspace mounts | Read-write | Read-only | None |
| Board context + sibling worktrees | Yes | No | No |
| Multi-turn with `--resume` | Yes | No | No |
| Session recovery on "no conversation" | Impl only | No | No |

### Incidental Differences (copy-paste drift)

These are accidental divergences that should be normalized:

1. **Flag order.** Oversight uses `-p <prompt> --output-format stream-json --verbose`; all others use `--verbose` before `--output-format`. Order is irrelevant to behavior.

2. **Container naming.** Ideation uses `wallfacer-ideate-{timestamp_ms}` while all others use `wallfacer-{role}-{uuid8}`. Timestamp-based names risk collisions under rapid succession.

3. **Timeout sources.** Hard-coded per role (title: 60s, oversight: 3m, commit: 90s, refinement: `constants.RefinementTimeout`, implementation: task timeout). Should be centralized in `internal/constants/`.

4. **Token-limit fallback structure.** All roles implement the same two-phase Claude→Codex retry, but oversight and ideation use slightly different wrapper structures (`oversightRunResult` vs inline). The logic is identical.

5. **Usage accumulation.** All roles follow the same `AccumulateSubAgentUsage()` + `AppendTurnUsage()` pattern — no actual drift here.

### Shared Across All 7 Roles

- NDJSON output format (`--output-format stream-json`)
- Same parse struct (`agentOutput`)
- Same usage tracking pattern
- Token-limit fallback (Claude → Codex)
- Per-activity sandbox routing via env vars
- Base container spec foundation (`buildBaseContainerSpec()`)

## Decision: Option A (Role Descriptors)

### Rationale

The divergences are almost entirely incidental. The fundamental differences reduce to 4 boolean/enum dimensions — this is parametric variance, not behavioral variance. A descriptor (data) fits better than an interface (behavior) because all 7 roles follow the exact same execution sequence:

> build spec → launch → read NDJSON → parse → accumulate usage

The only variance is in *what gets mounted* and *how the result string is interpreted*.

**Why not Option B (Interface)?** Roles don't have meaningfully different control flow. An interface is justified when implementations need fundamentally different execution models. Here, a descriptor captures all variance. If a future role truly needs a different execution model, it can bypass `runAgent()` entirely — the descriptor doesn't prevent that.

**Why not Options C/D yet?** No concrete use case demands inter-agent communication today. The abstraction is layered: Option A is independently valuable and doesn't preclude adding C/D later.

### Descriptor Shape

```go
type MountMode int

const (
    MountNone     MountMode = iota // Title, Oversight, Commit Msg
    MountReadOnly                   // Refinement, Ideation
    MountReadWrite                  // Implementation, Testing
)

type AgentRole struct {
    Activity    store.SandboxActivity
    PromptTmpl  string                        // template name in internal/prompts/
    Timeout     func(*store.Task) time.Duration
    MountMode   MountMode
    MountBoard  bool                          // include board.json + sibling worktrees
    SingleTurn  bool                          // no --resume loop
    ParseResult func(string) (any, error)     // role-specific output extraction
}
```

A central `runAgent(ctx, role AgentRole, task, prompt, opts)` handles:
- Sandbox selection (existing 4-tier hierarchy)
- Container spec building (dispatch on `MountMode` + `MountBoard`)
- Container lifecycle (register → launch → read → wait → parse NDJSON)
- Usage accumulation
- Token-limit fallback (Claude → Codex)

### Incidental Drift Fixes (included in refactor)

- Normalize flag order to `--verbose --output-format stream-json` everywhere
- Use UUID suffix for ideation container names
- Centralize all role timeouts in `internal/constants/`
- Extract token-limit fallback retry into a single shared helper

## Implementation Plan

### Phase 1 — Headless Roles (Title, Oversight, Commit Message)

Simplest tier: no mounts, single-turn. The three implementations are nearly identical modulo prompt template and result parsing.

1. Define `AgentRole` type and `MountMode` enum in a new file `internal/runner/agent.go`.
2. Implement `runAgent()` with support for `MountNone` only.
3. Define role descriptors: `roleTitle`, `roleOversight`, `roleCommitMessage`.
4. Migrate `GenerateTitle()` to call `runAgent(roleTitle, ...)` — remove duplicated spec-building and launch logic from `title.go`.
5. Migrate `runOversightAgent()` similarly — collapse `oversight.go` launch code.
6. Migrate `generateCommitMessage()` similarly — collapse `commit.go` launch code.
7. Verify all existing tests pass unchanged.

### Phase 2 — Inspector Roles (Refinement, Ideation)

Add `MountReadOnly` support to `runAgent()`.

1. Extend `runAgent()` to handle `MountMode: MountReadOnly` — mount all workspace directories read-only, mount instructions file.
2. Define `roleRefinement` and `roleIdeaAgent` descriptors.
3. Delete `buildRefinementContainerSpec()` and `buildIdeationContainerSpec()` — they are near-identical and collapse into `runAgent()`.
4. Migrate `RunRefinement()` and `RunIdeation()` to call `runAgent()`.
5. Verify all existing tests pass unchanged.

### Phase 3 — Heavyweight Roles (Implementation, Testing)

These have the most accretion (session recovery, auto-continue, verdict inference, worktree management). The container launch unifies; the turn loop and post-processing stay in their respective files.

1. Extend `runAgent()` to handle `MountMode: MountReadWrite` — mount worktrees read-write, mount board context and sibling worktrees.
2. Define `roleImplementation` and `roleTesting` descriptors with `SingleTurn: false`.
3. Refactor `runContainer()` to call `runAgent()` for each turn invocation. The turn loop, session management, and auto-continue logic remain in `execute.go` but no longer duplicate spec-building or launch code.
4. Verify all existing tests pass unchanged.

### Phase 4 — Cleanup

1. Remove dead code: orphaned spec builders, duplicated fallback helpers.
2. Consolidate container naming to `wallfacer-{role}-{uuid8}` pattern.
3. Centralize remaining hard-coded timeouts into `internal/constants/`.

## Deferred Work

The following are explicitly out of scope for this refactor but compatible with it:

- **Agent Interface (Option B):** Only needed if a future role requires fundamentally different control flow. The descriptor can be promoted to an interface at that point without breaking existing roles.
- **Agent Graph (Option C):** The existing implicit pipeline (implement → commit → title/oversight) could be made explicit as a graph, but the current hard-coded sequencing works and the graph adds complexity with no immediate payoff.
- **Inter-Agent Communication (Options D1/D2/D3):** No concrete use case demands it today. File-based messaging (D1) remains the simplest path if needed later.
- **User-Configurable Agent Pipelines:** Requires a configuration format, validation, and UI — significant scope that should be driven by concrete user demand.
