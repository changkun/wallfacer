# Technical Internals

These documents are for maintainers and contributors who need to understand how the system works internally. They cover architecture decisions, data models, execution flows, and the APIs that connect everything together.

Agents run as host processes: the runner execs the selected CLI directly with the task's git worktree as the working directory (`internal/executor/host.go`). There is no container daemon, image pull, or bind-mount at runtime.

Wallfacer runs on one machine with no hosted service behind it. The API is gated by a per-machine server key that the console receives on a loopback request.

## Reading Order

### 1. Architecture

[Architecture](architecture.md)

System overview, design decisions, component map, and an end-to-end walkthrough tracing a task from creation to merge. Covers the dispatch layer (the five built-in roles and their runner bindings). Package map covering all `internal/` and `internal/pkg/` packages. Handler organization table. Start here to build a mental model of how all the pieces fit together.

### 2. Data & Storage

[Data & Storage](data-and-storage.md)

Data models (Task, TaskEvent, TaskUsage, TaskSummary), per-task directory persistence, event sourcing, soft delete, search index, and the spec document model (`internal/spec/`).

### 3. Task Lifecycle

[Task Lifecycle](task-lifecycle.md)

The task state machine, turn loop, auto-retry, failure categorization, and board context manifests.

### 4. Git Operations & Worktree Lifecycle

[Git Operations & Worktree Lifecycle](git-worktrees.md)

Worktree management, the commit pipeline, branch operations, conflict resolution, stash handling, and worktree health restoration.

### 5. API & Transport

[API & Transport](api-and-transport.md)

Full HTTP route reference, SSE streaming, WebSocket terminal, Prometheus metrics, and middleware. Covers task, git, config, agent session, spec tree, explorer, image, and OAuth endpoints.

### 6. Automation

[Automation](automation.md)

Background watchers, the autoimplement promotion loop, auto-review, auto-submit, auto-retry, circuit breakers, and startup sequence.

### 7. Plan Mode

[Plan Mode](plan-mode.md)

Spec document model, spec tree building, agent-session sandbox, slash commands, dispatch pipeline, archive/unarchive, planning undo via git revert, and the SSE spec tree stream.

### 8. Workspaces & Configuration

[Workspaces & Configuration](workspaces-and-config.md)

Workspace manager, harness routing, system prompt templates, environment configuration, runtime workspace switching, and AGENTS.md instructions.

### 9. Development Setup

[Development Setup](development.md)

Building from source, running tests, make targets, and the release workflow.
