---
title: Docs Teardown & Rewrite
status: complete
depends_on: []
affects:
  - docs/
  - README.md
  - CONTRIBUTING.md
  - internal/cli/cli.go
  - internal/cli/server.go
  - internal/runner/
  - frontend/src/data/docs.ts
  - frontend/src/views/DocsIndex.vue
  - frontend/scripts/ui-shots/
effort: xlarge
created: 2026-07-05
updated: 2026-07-18
author: changkun
dispatched_task_id: null
---

# Docs Teardown & Rewrite

## Overview

The documentation set no longer describes the shipped application. This child
removes the current guide structure and re-architects the docs from the actual
surface: CLI commands, local-mode views, cloud mode, and the internals of the
system as it exists after the topos/agent-graph convergence and the idea-agent
teardown. Guides are rewritten in audience language and neutral tone (per
project style: no first/second person in user docs); internals stay precise and
deep. Screenshots regenerate in the new theme after
[design-tokens](design-tokens.md) lands (prose does not wait for it).

## Current State — confirmed drift

Documented but gone:
- brainstorm/idea-agent flow (`docs/guide/agents-and-flows.md`,
  `docs/internals/data-and-storage.md`, `docs/internals/automation.md`) — the
  subsystem was fully removed (`specs/local/remove-idea-agent-subsystem.md`);
  `internal/flow/registry.go` resolves legacy flow names to `implement`.
- Separate Agents and Flows pages — `/agents`, `/flows`, `/workflows` all
  redirect to `/agent-graph`; guides and README still describe the two-page
  model and its sidebar tabs.
- "ideation" as a distinct scheduled feature (11 files) — now a generic
  routine.
- Five agent roles including `test` — verify actual roles in
  `internal/agents/` (impl, title, oversight, commit-msg) and correct.
- `internal/runner` package comment still says "orchestrates ephemeral sandbox
  containers" under the host-process model.

Shipped but undocumented:
- Agent graph (`/agent-graph`, `internal/agentgraph`, embedded topos runtime,
  live agent traces).
- Whiteboard (`/whiteboard`, Excalidraw island, per-workspace scene).
- Mission Control (`/mission`, unified spec+task graph, inline lifecycle
  actions).
- GitHub integration (`internal/github`, settings surface).
- Device sign-in (`wallfacer auth`, RFC 8628, account menu modal) and the
  session-cookie behavior.
- `wallfacer web` command; `PrintUsage` in `internal/cli/cli.go` also omits
  `auth` and `web`.
- Workspace model redesign (stable-id workspaces, `workspaces.json`).

Structural problems:
- Dual nav index: server `parseReadingOrder` over `docs/guide/usage.md` +
  `docs/internals/internals.md` (local) vs static `docIndex` in
  `frontend/src/data/docs.ts` + hardcoded cards in `DocsIndex.vue` (cloud).
- `CONTRIBUTING.md` internals table omits `plan-mode.md`.
- Cloud DocPage bundles only `docs/guide/*.md` and rewrites internals links to
  GitHub — keep this split but make it explicit in the architecture.

## New Information Architecture

`docs/guide/` (user-facing, rewritten from scratch; old files deleted, slugs
chosen fresh — server redirects are not needed since docs URLs are unversioned,
but keep `getting-started` stable as the highest-inbound slug):

1. `getting-started.md` — install, doctor, first run, sign-in (device flow),
   first task end to end.
2. `concepts.md` — the mental model: workspaces, tasks, specs, agents, the
   autonomy spectrum (absorbs `autonomy-spectrum.md`).
3. `board.md` — task board, lifecycle, dependencies, search, bulk ops.
4. `chat.md` — chat surface, threads, slash commands, @mentions.
5. `plan.md` — spec mode: tree, lifecycle states, dispatch, drift pipeline
   (user-visible parts).
6. `agent-graph.md` — NEW: the unified agent surface, topos runtime, live
   traces, relationship to tasks.
7. `routines.md` — scheduled routines (replaces ideation framing).
8. `automation.md` — autoimplement, auto-test, auto-submit, retries, circuit
   breakers (absorbs `circuit-breakers.md`).
9. `oversight.md` — timelines, logs, diff review, inline comments, analytics
   and cost.
10. `mission-control.md` — NEW: the graph surface and acting from it.
11. `whiteboard.md` — NEW: Excalidraw canvas.
12. `workspaces.md` — workspace model (stable ids), git integration,
    branches, GitHub connect.
13. `configuration.md` — settings, env vars, harnesses (all five + selection),
    CLI reference (run/status/spec/doctor/auth/web), shortcuts.

`docs/internals/` — keep the strong 10-file skeleton, correct drift, add:
- `agent-graph-runtime.md` — NEW: `internal/agentgraph`, topos embedding,
  observer seam, lineage → timeline.
- `auth-and-identity.md` — NEW: OIDC/PKCE, device flow, session cookie,
  principal context (promote from `docs/cloud/`).
- Existing files re-verified against code (esp. `automation.md`,
  `data-and-storage.md` post idea-agent removal; `architecture.md` package
  map; fix `internal/runner` doc comment in code as part of this).

`docs/releases/` untouched (historical record). `docs/cloud/` folds into
internals + a short `cloud.md` guide note; `docs/origin.md` kept.

### Single-source nav

The server-parsed reading order remains canonical. Add a small build-time
generator: `frontend/scripts/gen-docs-index.mjs` parses the same "Reading
Order" section of `usage.md` (same rules as `parseReadingOrder`) and emits
`frontend/src/data/docs.ts` (checked in, CI-diffable like other generated
artifacts). `DocsIndex.vue` renders its cards from `docIndex` groups instead
of hardcoded lists. A Go test and a frontend test both parse the same fixture
to prevent rule drift.

## Components / Work Breakdown

1. **Code-truth pass** — read every surface (views, handlers, CLI, flow
   registry, agents) and produce the authoritative feature inventory; fix
   `PrintUsage`, the `internal/runner` doc comment, and `CONTRIBUTING.md`
   table in the same commit.
2. **Teardown + IA scaffold** — delete stale guides, write new `usage.md`
   reading order, scaffold new files with structure + frontmatter of each
   page (committable checkpoint; local docs nav renders the new tree).
3. **Guide rewrite** — content per file above, audience language, neutral
   tone, no first/second person; keyboard shortcuts and env vars verified
   against code.
4. **Internals refresh + new files** — precise, deep; verified against
   packages.
5. **Nav single-source** — generator + derived `docs.ts` + DocsIndex cards +
   tests.
6. **Screenshots** — after design-tokens: ui-shots light+dark pairs into
   `docs/guide/images/` (downscaled per ui-shots README), replacing all
   existing images; `public/static/overview-*` for the site.
7. **README + root docs** — README doc index regenerated; BUGS.md entries
   re-verified.

## Testing Strategy

- `go test ./internal/cli/...` — doc index/reading-order/search tests updated
  to the new tree; add a test that every slug in the reading order resolves to
  an embedded file and vice versa (orphan detection).
- Frontend: docs.ts generator fixture test; router/docs view tests keep
  passing; `bun run build` (DocPage glob picks up the new guide set).
- Link check: script (or extend gen-docs-index) that verifies every relative
  `.md` link and image path inside `docs/**` resolves; run in CI or as a Go
  test over the embedded FS.
- Manual: local `/docs` nav + cloud `/docs` cards render the same structure;
  docs search returns new content.

## Outcome

Shipped 2026-07-05 (commits 31ac0ff4, 7d3ef1e4, eba868d8, dbcadb00).
Executed via a code-truth feature inventory (Explore agent over the whole
surface) followed by six parallel writer agents. Deviations and notes:

- New 13-guide IA landed as specced; old guides deleted
  (autonomy-spectrum, exploring-ideas, designing-specs, board-and-tasks,
  refinement-and-ideation, agents-and-flows, circuit-breakers,
  oversight-and-analytics). docs/cloud/ was absorbed into the new
  internals/auth-and-identity.md and deleted (no separate cloud.md guide).
- Internals gained agent-graph-runtime.md and auth-and-identity.md plus a
  drift pass across six existing files (seven watchers incl. auto-review,
  five agent roles, stable-identity workspace model, expanded route
  groups).
- Nav single source: gen-docs-index.mjs derives data/docs.ts from the
  usage.md reading order; DocsIndex renders from it; a vitest guards sync
  and orphan slugs. The Go-side fixture test was not duplicated (the
  existing internal/cli docs tests already cover the parser).
- Code fixes folded in: PrintUsage lists auth/web, runner package doc no
  longer claims container execution, agents registry comment says five,
  GitHub broker comment matches the wiring, CONTRIBUTING internals table
  lists all 11 references.
- All screenshots regenerated in the new theme (guide image pairs incl.
  new mission/whiteboard surfaces, marketing statics, README assets).
- User-facing style enforced and grep-verified: no first/second person,
  no em dashes.
