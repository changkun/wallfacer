---
title: "Pure-Local Wallfacer: A Board Over the CLI Harnesses on One Machine"
status: validated
depends_on: []
affects:
  - internal/
  - frontend/src/
  - docs/
  - specs/
  - .github/workflows/release.yml
effort: xlarge
created: 2026-10-19
updated: 2026-10-19
author: changkun
dispatched_task_id: null
---

# Pure-Local Wallfacer: A Board Over the CLI Harnesses on One Machine

Decision record for a cleanup the maintainer started on 2026-10-19. Wallfacer
keeps its current shape, a task board that drives the coding-agent CLIs
installed on the machine, and sheds everything that existed for the Latere
platform or for features no longer wanted. It supersedes
[platform-native](../.archive/shared/platform-native.md), whose direction
(an application on the Latere platform with Topos as the only harness) is
reversed.

## In the maintainer's words

1. "evolve wallfacer on it's current shape. we focus on cleanup the repo, delete junk code, and simplify things. first task: remove artifacts."
2. "remove routine as well."
3. "Remove the entire cloud latere integration, login, coordination plane, topos execution, etc. make this tool pure local."
4. "Please remove container (podman/docker) mode."
5. "Remove Github integration."
6. "Remove/Archive: stale/outdated/invalid/suppresed specs." (read as superseded)
7. "i would be appreciate more aggresive cleanup and removal", answered by picking Whiteboard, Mission Control, Analytics, Terminal, review on a second model, the auto-pipeline toggles, the pre-changelog release notes and the complete specs, with "specs is special, all cleanup on specs should go specs/.archive".
8. "Remove keyboard shortcut system."
9. "Remove chat UI pls? We just need to preserve them in the popup and initial chat window when nothing is there."
10. On the terminal and the whiteboard as libraries: "when abstract as library, pls do not copy any code. Do a fresh implementation with best practice." and "for the excalidraw whiteboard, i am not sure we should depend on excalidraw, maybe let's defer this. i would love to have a fresh clean impl.", "same for terminal".
11. "focus on removal", answered by keeping every harness, the explorer, the git chip actions and Plan mode, and picking batch create, the manual test run, task dependencies, title generation, the in-app CLI sign-in, metrics and OpenTelemetry, prompt overrides, the embedded docs page and multi-folder workspaces.

## The removals

| # | Removal | Stored data and settings | State |
|---|---|---|---|
| 1 | **Artifacts gallery.** The feature was already gone from the code ([retire-artifacts](../.archive/shared/platform-native/retire-artifacts.md)); the tests that guarded its absence are deleted. | Files under a workspace's `artifacts/` directory are untouched. | Done |
| 2 | **Routines.** The scheduler, the `/api/routines` routes, the routine task fields, the Routines page, the routine card controls and the composer's Schedule toggle ([routine-tasks](../.archive/local/routine-tasks.md)). | A stored routine card (`kind: routine`) is skipped when the store loads: its file stays on disk unread and unrewritten, and the store logs one warning with the count. Tasks a routine spawned load as ordinary tasks; their `spawned-by:*` tag shows as a plain label. The one-time scheduled start of a backlog task went later, with row 14. | Done |
| 3 | **latere.ai sign-in and cloud mode.** Browser redirect sign-in (`/login`, `/callback`, `/logout`), device sign-in, `wallfacer auth`, `/api/me`, organization switching, `WALLFACER_CLOUD` and the `AUTH_*` settings, and per-principal and org scoping of tasks, events and workspaces. The per-machine server key is the board's one credential. | `created_by`, `org_id`, `actor_sub` and `actor_type` drop from a record the next time it is saved. `~/.wallfacer/cookie-key` and the shared `latere/token.json` under the user config directory stay on disk unread. | Done |
| 4 | **Coordination plane and spec comments.** The coordinator, the outbound connector, spec comments and the Postgres comment store. | `~/.wallfacer/coordination-opt-in` and `~/.wallfacer/instance-id` stay on disk unread. `WALLFACER_COORDINATION`, `WALLFACER_COORDINATION_URL` and `WALLFACER_DATABASE_*` are ignored. | Done |
| 5 | **Hosted server.** `wallfacer web` (wallfacerd), `deploy/`, `Dockerfile.wallfacerd`, and the image, deploy and smoke jobs of the release workflow. A release publishes the binaries only. | None. | Done |
| 6 | **Sandbox proxy.** The `/internal/sandbox-proxy/llm/*` relay for platform sandboxes and its service identity. | None. | Done |
| 7 | **Topos harness.** The in-process harness, the agent-graph seam, the trace view and `GET /api/tasks/{id}/trace`, the Lux model gateway path and the `latere.ai/x/topos` dependency ([topos-native-harness](../.archive/shared/topos-native-harness.md)). | A task or setting that still names `topos` runs on the default harness: harness resolution skips an unregistered harness and falls to the next tier. A stored trace drops from a task the next time it is saved. | Done |
| 8 | **Platform pieces in the console.** Browser telemetry, the language setting, the latere-ui dependency and the prerendered build; the console builds with plain `vite build`. | The theme stays in Settings. | Done |
| 9 | **Container execution.** The podman and docker execution mode and what remained of it: the process monitor at startup, the runtime command, worker stats, image fields, entrypoint scripts and the engine exit code. Agents run as host processes; a run with no workspace starts in a scratch directory. | Stored `container_crash` and `container_run` values still load. Code names for an agent process (`ContainerSpec`, `KillContainer`, `runContainer`) still say container; renaming them is a separate cleanup. | Done |
| 10 | **Specs.** Specs that are stale, describe a removed feature, are superseded, or have no path for a local tool move to `specs/.archive` with a note saying why; complete specs move there too. | None. | Done |
| 11 | **GitHub integration.** The GitHub settings tab, pull request panels and the `/api/github/*` and `/api/tasks/{id}/pr` routes. A branch is pushed with plain `git` and the host's credentials. | The token cache in `~/.wallfacer/github/` is deleted at first start. | Done |
| 12 | **Whiteboard, Mission Control, Analytics and the terminal.** The four console surfaces, their routes (`/api/whiteboard`, `/api/graph`, `/api/stats`, `/api/usage`, `/api/debug/spans`, `/api/terminal/ws`), `WALLFACER_TERMINAL_ENABLED`, `WALLFACER_AGENT_SESSION_WINDOW_DAYS`, and the Excalidraw, React, xterm and websocket dependencies. | Saved whiteboard scenes stay on disk unread. | Done |
| 13 | **The `/chat` page and the keyboard shortcut system.** Conversations live in the chat popup and in Plan's chat, which opens full width when a workspace has no specs. The command palette stays, opened from the rail. | None. | Done |
| 14 | **Automation and review.** The auto-pipeline watchers and their toggles, automatic retry, the circuit breaker, the scheduled start, critical-path ordering and review on a second model. A task moves only when someone moves it. | A task's `scheduled_at`, `review_*`, `auto_retry_*`, `test_fail_count`, `pending_test_feedback` and `last_fetch_error` fields, and a workspace's automation flags, drop from a record the next time it is saved. A line for a removed setting in the env file is ignored. Older timelines keep their `auto_*` triggers. | Done |
| 15 | **Task options.** Batch create, the manual test run, task dependencies and title generation: a task's title is the first line of its prompt. | To be recorded when the removal lands. | Planned |
| 16 | **Operations.** The in-app CLI sign-in (`internal/oauth`), the `/metrics` endpoint and OpenTelemetry export, prompt template overrides and the embedded docs page. Agents use the credentials the CLI already has or the env file. | Prompt override files stay on disk unread. `OTEL_*` settings are ignored. | Done |
| 17 | **Multi-folder workspaces.** A workspace is one folder, and agents no longer see other tasks' worktrees. | A workspace saved with several folders loads dormant with its history and opens once it is pointed at one folder. A task stored with several worktrees still commits into each. `WALLFACER_WORKSPACES` is ignored. | Done |
| 18 | **Release notes before the changelog.** `docs/releases` is deleted; those notes live on the GitHub releases. | None. | Done |

## What stays

- The five CLI harnesses (Claude, Codex, Cursor, OpenCode, Pi) run as host
  processes in per-task git worktrees.
- The task board, the spec tree and Plan mode, the file explorer, the git
  actions on the workspace chip, the chat popup, and the command palette.
- The lateregate CI bar, with the identity and Postgres roles `none`.

## Deferred

- A terminal and a whiteboard as fresh libraries, written from scratch with
  no code copied from Wallfacer and no xterm or Excalidraw dependency.
- A workspace as a sandbox policy: agents run unconfined on the host today,
  and `latere.ai/x/pkg/hostsandbox` could confine a run to the workspace's
  folders, an egress allowlist and an environment allowlist.
