---
title: Agent Memory & Identity
status: vague
depends_on:
  - specs/shared/agent-abstraction.md
  - specs/observability/telemetry-observability.md
affects:
  - internal/runner/
  - internal/store/
  - internal/prompts/
effort: xlarge
created: 2026-04-01
updated: 2026-04-01
author: changkun
dispatched_task_id: null
---

# Agent Memory & Identity

Design space exploration for giving wallfacer agents persistent memory that functions as identity construction rather than flat storage. Today each task agent starts blank — the only continuity is the workspace AGENTS.md and whatever the user manually writes into task prompts. This spec explores what changes when agents accumulate experience, develop preferences, and maintain narrative coherence across tasks.

Two inspirations frame the design space:

1. **Psychology's identity construction model** (Conway's self-memory system, Damasio's somatic markers): identity = memories organized by emotional significance, structured around self-images, continuously reconstructed to maintain narrative coherence. Memory is not a filing cabinet — it's a process that edits itself to serve current goals.

2. **Claude Code's auto-memory system**: file-based persistence with a two-level index (MEMORY.md → topic files), background extraction agents, four-type taxonomy (user, feedback, project, reference), and a derivability test that excludes anything recoverable from the current project state.

The core question: what happens when wallfacer stops treating each task as amnesiac and starts building a shared identity that accumulates across all task executions within a workspace?

---

## Design Space

### 1. Memory as Identity, Not Storage

Current AI memory systems (including Claude Code's) treat memory as a key-value store with metadata. The psychology literature suggests memory should be:

- **Hierarchically organized**: interaction epochs → recurring themes → specific exchanges (Conway's lifetime periods → general events → event-specific knowledge)
- **Goal-filtered**: retrieval weighted by relevance to the current task, not just semantic similarity
- **Emotionally weighted**: high-surprise, high-consequence events (task failures, architectural discoveries, user corrections) remembered more strongly than routine completions
- **Narratively coherent**: memories synthesized into a story about the project, not a bag of facts

**What this means for wallfacer:**

A task agent working on `internal/runner/` should not just know "the runner package exists." It should know "the runner package has been refactored twice this month, the last refactor broke worktree sync, and the user prefers small commits when touching this area." That's identity-informed context, not fact retrieval.

**Key tension:** Richer memory means higher token cost per task. The psychology model says filter by relevance to current goals — but determining relevance itself costs tokens.

---

### 2. Memory Taxonomy for Wallfacer

Claude Code's four-type taxonomy (user, feedback, project, reference) is a reasonable starting point but needs adaptation for a multi-agent task system.

**Proposed taxonomy:**

| Type | What it captures | Example |
|------|-----------------|---------|
| **workspace** | Patterns, conventions, and codebase knowledge specific to a workspace | "The auth middleware uses JWT stored in httpOnly cookies, not localStorage" |
| **behavioral** | Corrections and confirmed approaches from past task executions | "When touching internal/runner/, always run the full test suite — unit tests alone miss integration failures" |
| **relational** | Who the user is, how they work, what they care about | "User prefers terse PR descriptions. Reviews code before committing. Values test coverage highly." |
| **experiential** | Significant events: failures, surprises, breakthroughs | "Task abc123 failed because the migration assumed PostgreSQL but the workspace uses SQLite — never assume database engine" |
| **capability** | What's been built, what's available, what patterns exist | "internal/pkg/dag/ provides generic DAG operations — don't reimplement cycle detection" |

**Difference from Claude Code:** Claude Code's memory is per-user-per-project. Wallfacer's memory would be per-workspace, shared across all task agents running in that workspace. This is the "shared world model" from the intelligence system spec — memory is one of its substrates.

**Open question:** Should some memory types be per-task (experiential) and others per-workspace (workspace, capability)? Or should all memory be workspace-scoped with task provenance metadata?

---

### 3. The Reminiscence Bump: Formative Moments

The psychology literature shows humans remember transitions disproportionately — the moments they became someone new. For an AI system, the analog is:

- First encounter with a new codebase pattern
- A task failure that revealed a hidden assumption
- A user correction that changed how the system approaches a class of problems
- An architectural decision that constrained all future work

**What this means for wallfacer:**

Not all task completions are equal. A routine bug fix that passes on the first try is forgettable. A task that failed three times, required a user correction about database assumptions, and eventually revealed that the test fixtures were stale — that's a formative moment worth weighting heavily in future retrievals.

**Possible implementation:**
- Tag memories with a **salience score** based on: surprise (did the outcome differ from expectation?), consequence (did this change downstream behavior?), correction (did the user intervene?), cost (how many tokens/retries did this take?)
- Weight retrieval by salience, not just recency or semantic similarity
- Periodically consolidate low-salience memories, keep high-salience ones intact

---

### 4. Emotional Weighting via Somatic Markers

Damasio's somatic marker hypothesis: emotions are prerequisites for rational decisions. The brain reactivates physiological states from past outcomes before conscious deliberation.

AI agents don't have emotions, but they have analogs:

| Human concept | Agent analog |
|---------------|-------------|
| Fear / anxiety | High failure rate on similar past tasks |
| Confidence | Consistent success with this type of change |
| Surprise | Unexpected test failure, unexpected user feedback |
| Frustration | Repeated retries, escalating token cost |

**What this means for wallfacer:**

Before starting a task, the agent could query memory for "somatic markers" — signals from past similar tasks:

- "Tasks touching internal/runner/ have a 40% failure rate" → allocate more budget, run tests early
- "The user always corrects formatting in this package" → apply stricter formatting from the start
- "Last time we changed this interface, three downstream tasks broke" → check dependents first

This connects directly to the intelligence system's "learning from failure patterns" idea, but grounds it in per-task retrieval rather than aggregate dashboards.

---

### 5. Narrative Coherence: The Project Story

Individual memories are fragments. Identity requires narrative — a coherent story that explains where the project is, how it got here, and where it's going.

**What narrative coherence provides:**
- A new task agent can orient itself in seconds instead of rediscovering context from scratch
- Contradictory memories surface as narrative inconsistencies rather than silent conflicts
- The "project story" evolves as work progresses, providing a living alternative to stale documentation

**Possible implementation:**
- A periodically-regenerated **project narrative** — a natural-language summary synthesized from workspace memories, recent task outcomes, and spec progress
- The narrative is not a memory itself but a computed view, regenerated when significant events occur (task completion, spec status change, user correction)
- Task agents receive the narrative as part of their system prompt, replacing or augmenting the current board manifest

**Key tension:** Narrative generation costs tokens. How often is "periodically"? On every task start? Daily? On-demand?

---

### 6. Co-Emergent Self-Model

The most speculative idea from the psychology literature. Current memory systems model "what I know about the user" but not "who I am in this relationship."

**What this could mean for wallfacer:**

Each workspace develops a working identity — not just facts about the codebase but a sense of how agents should behave in this context:

- "In this workspace, we write tests before implementation"
- "This user trusts agents with refactoring but wants to review all new APIs"
- "We favor readability over performance unless profiling says otherwise"

This is subtly different from behavioral memory (which records specific corrections). The self-model is a synthesized stance — what the agent *is* in this workspace, not just what it's been told.

**Implementation spectrum:**
- **Minimal:** Behavioral memories auto-summarized into a "workspace personality" prompt section
- **Medium:** Explicit workspace identity document that evolves based on accumulated behavioral memories, with user review
- **Maximal:** Agents calibrate their own confidence, risk tolerance, and communication style based on the self-model

---

### 7. Memory Extraction and Lifecycle

When and how do memories get created, updated, and retired?

**Extraction triggers (when to write):**
- Task completion (success or failure) → experiential memory
- User feedback on a waiting task → behavioral memory
- New package or API created → capability memory
- User correction or confirmation → relational memory
- Unexpected outcome (test failure after expected success) → experiential memory with high salience

**Extraction mechanism:**
- **Background agent** (Claude Code model): a lightweight agent inspects task output after completion and extracts memories. Runs asynchronously, doesn't block the task pipeline.
- **Inline extraction**: the task agent itself decides what's worth remembering during execution. Lower latency but adds token cost to every task.
- **Human-triggered**: user marks a task outcome as "remember this." Explicit but relies on user attention.

**Memory lifecycle:**
- **Creation:** Memories are written to per-workspace storage (file-based, like Claude Code's model)
- **Consolidation:** Periodic pass merges related memories, removes duplicates, updates salience scores
- **Staleness:** Memories about code that no longer exists get flagged. The derivability test applies: if the memory can be derived from current project state, it's noise.
- **Retirement:** Low-salience, old memories eventually get archived (not deleted — available for narrative reconstruction but not active retrieval)

---

### 8. Memory Storage and Retrieval

**Storage format:**
- File-based (one file per memory, like Claude Code) vs structured store (SQLite, tied to telemetry)
- Index file mapping memory IDs to topics for fast scanning
- Provenance metadata: which task created this memory, when, what triggered it

**Retrieval strategies:**
- **Goal-filtered:** Given the current task's prompt and goal, retrieve memories relevant to this specific work
- **File-scoped:** Given the files this task will likely touch (from spec `affects` or early planning), retrieve memories about those files
- **Salience-weighted:** Prefer high-salience memories over low-salience ones
- **Recency-decayed:** Recent memories weighted higher, but formative moments resist decay

**Token budget for memory:**
- How many tokens of memory context can a task afford? This needs empirical data.
- Possible approach: start with a fixed budget (e.g., 2000 tokens), measure impact on task success rate, adjust

---

### 9. Relationship to Intelligence System

This spec provides a foundation for the intelligence system's shared world model. Specifically:

| Intelligence system component | Memory system contribution |
|-------------------------------|---------------------------|
| Project world model | Workspace memories + project narrative provide the "what we know" layer |
| Cross-task awareness | Experiential memories about file conflicts and interface changes inform conflict prediction |
| Capability registry | Capability memories track what's been built, queryable by future tasks |
| Smarter human-in-the-loop | Relational memories inform when and how to ask for human input |
| Learning from failure | Experiential memories with somatic markers aggregate into failure patterns |
| Shared context bus | Memory becomes a persistence layer for the context bus — discoveries written as memories, queryable by other tasks |

The memory system is one of the substrates the intelligence system builds on. The other substrates (telemetry, agent abstraction, multi-agent consensus) provide the infrastructure; memory provides the content.

---

## Open Questions

- **Token economics:** How much memory context improves task success rate vs the token cost? Need A/B experiments comparing amnesiac tasks vs memory-augmented tasks.
- **Privacy and scope:** Should workspace memory be visible to the user? Editable? Should the user be able to suppress specific memories?
- **Cross-workspace transfer:** Do some memories (relational, behavioral) transfer across workspaces? Or is each workspace a clean slate?
- **Memory conflicts:** When two tasks produce contradictory memories (e.g., "always use PostgreSQL" vs "this project uses SQLite"), how is the conflict resolved?
- **Evaluation:** How do we measure whether memory-augmented agents perform better? Task success rate? Token efficiency? Fewer user interventions?
- **Minimum viable memory:** Which memory types deliver standalone value? Behavioral and capability memories seem highest-ROI. Narrative coherence and self-model may only pay off at scale.
- **Interaction with AGENTS.md:** Today the workspace AGENTS.md is the closest thing to persistent context. Should memory replace it, augment it, or auto-generate parts of it?
