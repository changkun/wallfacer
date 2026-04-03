# Technical Internals

These documents are for maintainers and contributors who need to understand how the system works internally. They cover architecture decisions, data models, execution flows, and the APIs that connect everything together.

## Reading Order

### 1. Architecture

[Architecture](architecture.md)

System overview, design decisions, component map, and an end-to-end walkthrough tracing a task from creation to merge. Package map covering all `internal/` and `internal/pkg/` packages. Handler organisation table. Start here to build a mental model of how all the pieces fit together.

### 2. Data & Storage

[Data & Storage](data-and-storage.md)

Data models (Task, TaskEvent, TaskUsage, TaskSummary), per-task directory persistence, event sourcing, soft delete, search index, and the spec document model (`internal/spec/`).

### 3. Task Lifecycle

[Task Lifecycle](task-lifecycle.md)

The task state machine, turn loop, dependency resolution, auto-retry, failure categorization, and board context manifests.

### 4. Git Operations

[Git Operations](git-worktrees.md)

Worktree management, the commit pipeline, branch operations, conflict resolution, stash handling, and worktree health restoration.

### 5. API & Transport

[API & Transport](api-and-transport.md)

Full HTTP route reference (97 routes), SSE streaming, WebSocket terminal, Prometheus metrics, and middleware. Covers task, git, config, planning, spec tree, explorer, image, and OAuth endpoints.

### 6. Automation

[Automation](automation.md)

Background watchers, the autopilot promotion loop, auto-test, auto-submit, auto-retry, circuit breakers, ideation scheduling, and startup sequence.

### 7. Workspaces & Configuration

[Workspaces & Configuration](workspaces-and-config.md)

Workspace manager, sandbox routing, system prompt templates, environment configuration, runtime workspace switching, and AGENTS.md instructions.

### 8. Development Setup

[Development Setup](development.md)

Building from source, running tests, make targets, sandbox images, and the release workflow.
