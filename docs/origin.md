# Origin

Wallfacer started as a practical response to a repeated workflow: write a task prompt, run an agent, inspect output, and do it again. The bottleneck was not coding speed, it was coordination and visibility across many concurrent agent tasks. A task board became the control surface.

The first version was a Go server with a minimal web UI. Tasks moved from backlog to in progress, executed in isolated git worktrees as host processes, and landed in done when complete. Each task got its own worktree, so many tasks could run in parallel on the same repository without colliding.

Then the system kept growing into its own gaps. The execution engine gained process reuse, circuit breakers, dependency caching, and multi-workspace groups. An autonomous loop handles implementation, testing, auto-retry, and autopilot promotion. An oversight layer (live logs, timelines, traces, diffs, and per-turn cost breakdown) ensures every agent decision is auditable before results are accepted.

Work is no longer one monolithic prompt per task. A dispatch layer composes specialized built-in agents (title, oversight, commit message, ideation, implementation, testing) into flows, so a single run can implement, test, then write a commit message, title, and oversight summary in one pass.

As the projects grew in complexity, raw task prompts became insufficient. Design specs emerged as the thinking layer between ideas and executable tasks: structured documents with lifecycle states, dependency graphs, and recursive progress tracking. A planning chat agent made specs conversational: explore an idea in chat, iterate on the design, break it into tasks, dispatch to the board. The same chat handles prompt refinement, editing a task's instructions in place rather than through a separate flow.

The integrated development environment now includes a file explorer with editor, an interactive host terminal, system prompt customization, and prompt templates, all accessible from the browser.

The latest evolution is cloud mode: sign-in identity with organization scoping, so tasks carry an owner and an org, and the board shows each principal only the work that belongs to their tenant.

Most of Wallfacer's recent capabilities were developed by Wallfacer itself, creating a compounding loop where the system continuously improves its own engineering process.
