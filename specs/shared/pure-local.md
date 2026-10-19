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
5. "Remove/Archive: stale/outdated/invalid/suppresed specs." (read as superseded)

## The removals

| # | Removal | Stored data and settings | State |
|---|---|---|---|
| 1 | **Artifacts gallery.** The feature was already gone from the code ([retire-artifacts](../.archive/shared/platform-native/retire-artifacts.md)); the tests that guarded its absence are deleted. | Files under a workspace's `artifacts/` directory are untouched. | Done |
| 2 | **Routines.** The scheduler, the `/api/routines` routes, the routine task fields, the Routines page, the routine card controls and the composer's Schedule toggle ([routine-tasks](../.archive/local/routine-tasks.md)). | A stored routine card (`kind: routine`) is skipped when the store loads: its file stays on disk unread and unrewritten, and the store logs one warning with the count. Tasks a routine spawned load as ordinary tasks; their `spawned-by:*` tag shows as a plain label. The one-time scheduled start of a backlog task is a separate feature and stays. | Done |
| 3 | **latere.ai sign-in and cloud mode.** Browser redirect sign-in (`/login`, `/callback`, `/logout`), device sign-in, `wallfacer auth`, `/api/me`, organization switching, `WALLFACER_CLOUD` and the `AUTH_*` settings, and per-principal and org scoping of tasks, events and workspaces. The per-machine server key is the board's one credential. | `created_by`, `org_id`, `actor_sub` and `actor_type` drop from a record the next time it is saved. `~/.wallfacer/cookie-key` and the shared `latere/token.json` under the user config directory stay on disk unread. | Done |
| 4 | **Coordination plane and spec comments.** The coordinator, the outbound connector, spec comments and the Postgres comment store. | `~/.wallfacer/coordination-opt-in` and `~/.wallfacer/instance-id` stay on disk unread. `WALLFACER_COORDINATION`, `WALLFACER_COORDINATION_URL` and `WALLFACER_DATABASE_*` are ignored. | Done |
| 5 | **Hosted server.** `wallfacer web` (wallfacerd), `deploy/`, `Dockerfile.wallfacerd`, and the image, deploy and smoke jobs of the release workflow. A release publishes the binaries only. | None. | Done |
| 6 | **Sandbox proxy.** The `/internal/sandbox-proxy/llm/*` relay for platform sandboxes and its service identity. | None. | Done |
| 7 | **Topos harness.** The in-process harness, the agent-graph seam, the trace view and `GET /api/tasks/{id}/trace`, the Lux model gateway path and the `latere.ai/x/topos` dependency ([topos-native-harness](../.archive/shared/topos-native-harness.md)). | A task or setting that still names `topos` runs on the default harness: harness resolution skips an unregistered harness and falls to the next tier. A stored trace drops from a task the next time it is saved. | Done |
| 8 | **Platform pieces in the console.** Browser telemetry, the language setting, the latere-ui dependency and the prerendered build; the console builds with plain `vite build`. | The theme stays in Settings. | Done |
| 9 | **Container execution.** The podman and docker execution mode and what remains of it. | To be recorded when the removal lands. | In progress |
| 10 | **Specs.** Specs that are stale, describe a removed feature, are superseded, or have no path for a local tool move to `specs/.archive` with a note saying why. | None. | Done |

## What stays

- The five CLI harnesses (Claude, Codex, Cursor, OpenCode, Pi) run as host
  processes in per-task git worktrees.
- The Claude and OpenAI provider sign-in under `/api/auth/{provider}/...`,
  which feeds the CLI harnesses' credentials.
- Mission Control (`GET /api/graph`), review on a second model, the spec
  tree and Plan mode, the terminal, the explorer and the whiteboard.
- Server-side OpenTelemetry export, which stays off unless
  `OTEL_EXPORTER_OTLP_ENDPOINT` is set.
- The lateregate CI bar, with the identity role `none`.
