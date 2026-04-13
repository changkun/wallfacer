# The Autonomy Spectrum

Every AI coding tool pins you to one interaction mode. Some are chatbots that require constant hand-holding. Others are fire-and-forget agents that disappear into a container and surface hours later with unpredictable results. Wallfacer gives you a continuous spectrum between these extremes, and lets you move freely along it depending on what the work demands.

```mermaid
graph LR
  Chat["💬 Chat\nExplore ideas"] --> Spec["📋 Spec\nDesign structure"]
  Spec --> Task["⚡ Task\nExecute & test"]
  Task --> Code["🔧 Code\nDirect control"]
```

---

## The Four Levels

Wallfacer organizes work into four levels, from highest autonomy to most direct control.

### Chat (Conversational Exploration)

You describe what you want in natural language. The agent shapes ideas, explores trade-offs, and proposes directions. This is the entry point for greenfield work -- when you do not yet know what to build.

The planning chat (accessible in Plan mode) is a persistent conversation backed by a sandbox container. It can read your codebase, create files, and execute commands while you steer the direction.

### Spec (Structured Design)

Ideas crystallize into structured documents with lifecycle states, dependencies, and acceptance criteria. Specs track progress through a six-state lifecycle: the main axis runs vague → drafted → validated → complete, with `stale` and `archived` as off-axis states for designs that have drifted from reality or been set aside. Transitions are not free-form — they are enforced by a server-side state machine (`internal/spec/lifecycle.go`) that rejects illegal jumps and keeps dispatched work consistent with the underlying spec.

At this level, agents iterate on design rather than code. They break large specs into sub-specs, validate consistency across the dependency graph, and analyze cross-impacts with existing plans. The output is a blueprint, not a pull request.

### Task (Managed Execution)

Specs break into executable tasks on a kanban board. Each task runs an agent in an isolated sandbox container. The agent implements, tests, and commits changes on a dedicated branch. You review diffs, oversight summaries, and test verdicts before accepting the work.

This is where most day-to-day work happens. Tasks are concrete, trackable, and independently testable.

### Code (Direct Control)

The file explorer, integrated terminal, and inline editor give you direct access to workspace files. Drop into the code when you need surgical precision -- fix a typo, inspect a log, or manually resolve a conflict.

No agent is involved at this level unless you choose to invoke one.

---

## The Autonomy Dial

At each level, you choose how much freedom the agent gets. Three broad modes exist:

```mermaid
graph TD
  subgraph Manual["🔒 Manual"]
    M1["Create tasks yourself"]
    M2["Review every diff"]
    M3["Approve every commit"]
  end
  subgraph Semi["⚖️ Semi-Auto"]
    S1["Auto-test on completion"]
    S2["Auto-retry on failure"]
    S3["Manual approve & push"]
  end
  subgraph Full["🚀 Full Auto"]
    F1["Autopilot promotes backlog"]
    F2["Auto-submit on pass"]
    F3["Auto-push to remote"]
  end
  Manual --> Semi --> Full
```

**Manual** -- You create tasks yourself, review every diff, approve every commit. Maximum control, minimum throughput.

**Semi-automatic** -- Agents execute and test automatically, but you approve before changes are merged. Auto-test catches regressions; auto-retry handles transient failures. You still review and accept.

**Full automatic** -- Autopilot promotes backlog tasks as capacity opens. Auto-submit merges passing work without approval. Auto-push sends changes to the remote. You monitor rather than manage.

These modes are not discrete settings but composable toggles. Enable auto-test without auto-submit. Turn on autopilot but keep auto-push off. The combination you choose defines where you sit on the spectrum for any given session.

---

## Moving Between Levels

You do not have to start at chat and work down. Jump to any level based on what you know:

- **Got a clear spec?** Dispatch it directly to the task board.
- **Want to explore a vague idea?** Start in the planning chat.
- **Know exactly what to fix?** Create a task with a concrete prompt.
- **Need to edit one line?** Open the file explorer or terminal.

Nothing stops you from starting a task, switching to the terminal to debug something mid-run, providing feedback based on what you see, and then letting the agent continue. The levels are access points, not a mandatory pipeline.

---

## Moving Up and Down

Work flows naturally between levels in both directions:

- **Up (more autonomy):** A chat conversation produces a spec. The spec is broken down into tasks. Tasks execute automatically.
- **Down (more control):** A failing task surfaces a problem. You inspect the diff, open the terminal to reproduce it, fix the issue manually, then let the agent continue from the corrected state.

The value of the spectrum is that you spend your attention where it matters and delegate the rest.

---

## Self-Development

Wallfacer builds itself using this spectrum. Most recent features -- the spec explorer, planning chat, file editor, dependency graph -- were designed as specs, broken into tasks, and implemented by Wallfacer's own agents running on its own task board.

The workflow you use is the same workflow the project uses to evolve itself.

---

## See Also

- [Exploring Ideas](exploring-ideas.md) -- the planning chat (Chat level)
- [Designing Specs](designing-specs.md) -- structured design (Spec level)
- [Board & Tasks](board-and-tasks.md) -- managed execution (Task level)
- [Configuration](configuration.md) -- automation toggles for the autonomy dial
