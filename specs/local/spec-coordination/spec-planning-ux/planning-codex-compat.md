---
title: Planning Chat Agent — Codex Compatibility
status: vague
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/planning-chat-agent.md
affects:
  - internal/planner/
  - internal/runner/
effort: medium
created: 2026-04-03
updated: 2026-04-03
author: changkun
dispatched_task_id: null
---

# Planning Chat Agent — Codex Compatibility

## Design Problem

The planning chat agent (see `planning-chat-agent.md`) is built on Claude Code's headless mode (`-p --resume --output-format stream-json`). Codex's headless mode may have different CLI flags, session resumption mechanisms, and output formats. This spec addresses what's needed to make the planning chat work with Codex as an alternative sandbox backend.

## Open Questions

1. What is Codex's equivalent of `--resume <session-id>` for multi-turn conversation continuity? Does it support session persistence at all?
2. Does Codex support `--output-format stream-json`, or does it use a different streaming format? The task runner already branches on sandbox type in `internal/runner/container.go` — the planner may need similar branching.
3. Where does Codex store session state inside the container? Claude Code uses `~/.claude/` (mounted as the `claude-config` named volume). Codex may need its own volume.
4. Are Codex's file-writing permissions compatible with the planning sandbox mount layout (full workspace RO, `specs/` RW)?
