# Technical Internals

These documents are for maintainers and contributors who need to understand how the system works internally. They cover architecture decisions, data models, execution flows, and the APIs that connect everything together.

## Reading Order

### 1. Architecture

[Architecture](architecture.md)

System overview, design decisions, component map, and an end-to-end walkthrough tracing a task from creation to merge. Start here to build a mental model of how all the pieces fit together.

### 2. Data & Storage

[Data & Storage](data-and-storage.md)

Data models, per-task directory persistence, event sourcing, soft delete, and the search index.

### 3. Task Lifecycle

[Task Lifecycle](task-lifecycle.md)

The task state machine, turn loop, dependency resolution, auto-retry, and board context manifests.

### 4. Git Operations

[Git Operations](git-worktrees.md)

Worktree management, the commit pipeline, branch operations, conflict resolution, and stash handling.

### 5. API & Transport

[API & Transport](api-and-transport.md)

HTTP route reference, SSE streaming, webhook notifications, Prometheus metrics, and middleware.

### 6. Automation

[Automation](automation.md)

Background watchers, the autopilot promotion loop, auto-test, auto-submit, circuit breakers, and startup sequence.

### 7. Workspaces & Configuration

[Workspaces & Configuration](workspaces-and-config.md)

Workspace manager, sandbox routing, system prompt templates, environment configuration, and runtime workspace switching.

### 8. Development Setup

[Development Setup](development.md)

Building from source, running tests, make targets, sandbox images, and the release workflow.
