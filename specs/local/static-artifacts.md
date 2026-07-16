---
title: "Static Artifacts - Serve Self-Contained HTML from Wallfacer"
status: drafted
depends_on: []
affects:
  - internal/handler/artifacts.go
  - internal/cli/server.go
  - frontend/src/views/ArtifactsView.vue
  - frontend/src/router.ts
  - frontend/src/components/Sidebar.vue
  - docs/guide/artifacts.md
effort: medium
created: 2026-07-15
updated: 2026-07-15
author: changkun
dispatched_task_id: null
---

# Static Artifacts - Serve Self-Contained HTML from Wallfacer

## Problem

Agents and users produce self-contained HTML deliverables - slide decks, reports, dashboards, one-off visualizations. Today there is no way to open one inside Wallfacer: the file lands in the repo and must be opened by hand from a file browser. The desired flow is direct: ask in chat for a deck, the agent writes it into the repo, and it opens in the app with one click.

This is the lightweight end of [live-serve.md](live-serve.md). That spec (stale, unbuilt) covers running and building developed software - dev servers, build pipelines, long-lived processes. Static artifacts need none of that: no build step, no process, no port. Just static files served over the app's existing HTTP server. Applications that need a backend remain future work under live-serve.

## Scope

In scope: serve static, self-contained web files (HTML and the assets an HTML file may reference) from a per-workspace directory, list them, and view them in the app.

Out of scope (deferred to live-serve): anything requiring a build step, a long-lived process, a bound port, or a runtime beyond static file delivery.

## Design

### Directory convention

Artifacts live at `<workspace>/artifacts/`, resolved against the first configured workspace folder (`workspaces[0]`). This is deliberate, not incidental: chat and spec agents run with their working directory set to the workspace root (`workspaces[0]`), so an agent instructed to write `artifacts/deck.html` lands the file exactly where the server reads it, with no extra wiring. Serving only `workspaces[0]` (rather than searching every folder) keeps the `/artifact/<path>` URL unambiguous when a multi-folder workspace holds the same filename twice.

A task agent writes into its isolated git worktree, so a task-produced artifact appears at the workspace root only after its branch is committed and merged. The reliable "create then open" surface is therefore chat and spec.

### Serving

Two routes, registered directly on the mux alongside the docs routes (which are also direct, not part of the API contract):

- `GET /api/artifacts` - list artifacts under `<workspace[0]>/artifacts/`.
- `GET /artifact/{path...}` - serve one artifact file as static content.

The raw-content route is singular (`/artifact/`) so it never collides with the client-side gallery page at `/artifacts` (the SPA catch-all `GET /` serves that route).

Containment uses `os.OpenRoot` (Go 1.24+): the artifacts directory is opened as an `*os.Root`, and file access is confined to it. `..` segments and symlinks that escape the root are rejected by the runtime, which is stronger than a string-prefix check. A content-type whitelist (html, css, js, json, svg, and common image and font types) is applied explicitly, so a stray `.go` or other source file under `artifacts/` is never served and the response is never MIME-sniffed. Responses carry `Cache-Control: no-store` so re-rendering an artifact in place is reflected immediately rather than served stale.

### UI

A gallery page (`/artifacts`, sidebar entry) lists artifacts newest-first. Selecting one shows an inline `<iframe>` preview (with `allow="fullscreen"`) and an "Open" action that opens `/artifact/<path>` in a new browser tab.

The new tab is the presentation surface; the iframe is a preview. A full-screen keyboard-driven deck binds `keydown` on the document and uses the Fullscreen API, both of which are constrained inside an iframe (focus must be clicked into it; fullscreen needs the `allow` attribute). Routing "present" through the new tab sidesteps both.

## Security

- **Containment**: `os.OpenRoot` confines reads to `<workspace[0]>/artifacts/`; traversal and escaping symlinks are rejected.
- **Content policy**: only whitelisted web content-types are served; other files return 404.
- **Same-origin exposure**: artifacts are served from the app's own origin, so an artifact's JavaScript can call authenticated `/api/*` with the user's session. This is acceptable for the local single-user product. Before this is exposed in cloud or principal-scoped mode, artifacts should move to a separate origin or port (isolation is deferred).

## Acceptance Criteria

- `GET /artifact/wallfacer-talk.html` returns `200` with `Content-Type: text/html; charset=utf-8` and the file bytes when the deck is present at `<workspace>/artifacts/`.
- A traversal attempt (`GET /artifact/../go.mod`) does not return repository contents.
- A non-whitelisted extension under `artifacts/` returns 404.
- `GET /api/artifacts` lists the HTML files under `<workspace[0]>/artifacts/` and excludes non-web files.
- The gallery lists artifacts, previews the selected one in an iframe, and opens it in a new tab.

## Phases

1. Backend: `internal/handler/artifacts.go` (`ListArtifacts`, `ServeArtifact`), route registration in `internal/cli/server.go`, handler tests. Prove end-to-end by serving the committed deck.
2. Frontend: `ArtifactsView.vue`, router entry, sidebar entry, iframe preview + open-in-tab.
3. Docs: `docs/guide/artifacts.md`, README/usage link, CLAUDE.md route note.

## Out of Scope / Future

- Build steps, processes, ports, backends (see live-serve).
- Cross-origin isolation for cloud/principal mode.
- Editing or authoring artifacts in the app (they are produced by agents or by hand).
- Per-task-worktree artifact serving before merge.
