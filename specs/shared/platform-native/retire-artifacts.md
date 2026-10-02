---
title: Retire the Artifacts Gallery
status: drafted
depends_on:
  - specs/shared/platform-native.md
affects:
  - internal/handler/artifacts.go
  - internal/cli/server.go
  - internal/handler/middleware.go
  - frontend/src/views/ArtifactsView.vue
  - frontend/src/router.ts
  - frontend/src/lib/nav.ts
  - frontend/scripts/ui-shots/
  - docs/
effort: small
created: 2026-10-02
updated: 2026-10-02
author: changkun
dispatched_task_id: null
---

# Retire the Artifacts Gallery

A decision of the maintainer on 2026-10-02, recorded under
[platform-native](../platform-native.md): wallfacer does not need its own
artifacts feature, because the Latere platform's Apps capability hosts web
pages from a repository. A removal, not a rewrite.

## What goes

The feature [static-artifacts](../../.archive/local/static-artifacts.md)
shipped: a gallery at `/artifacts` that lists the self-contained HTML files
under `<workspace>/artifacts/` and previews them, served by
`GET /artifact/{path...}` and listed by `GET /api/artifacts`.

## Why

- **Publishing is the platform's.** Apps builds a preview from every push to
  a repository on the platform's git host and releases a tag at an address of
  its own. A page an agent made, a deck or a report, is published that way
  when the user wants it seen.
- **Looking at a file locally needs no feature.** The file is in the
  workspace; the file explorer shows it and a browser opens it.
- **It carried a risk that blocked any hosted use.** Artifacts were served
  from wallfacer's own origin, so a page's script could call wallfacer's API
  with the viewer's session. The spec accepted that for a local single-user
  instance only. Removing the feature removes the risk.

## Decisions

1. **The routes, the page and the rail entry go.** `GET /api/artifacts` and
   `GET /artifact/{path...}` answer 404 like any unknown path; `/artifacts`
   renders the console's not-found page.
2. **Files are untouched.** `<workspace>/artifacts/` belongs to the user's
   repository. Wallfacer never wrote it and does not delete it.
3. **No Apps integration in wallfacer.** Publishing through Apps is a push to
   a repository; an agent or the user does it with git. Wallfacer adds no
   Apps client here.

## Surface removed

- `internal/handler/artifacts.go` and its tests; the two route registrations
  in `internal/cli/server.go`; `/artifacts` in the public UI path list in
  `internal/handler/middleware.go`.
- `frontend/src/views/ArtifactsView.vue`, its route in `router.ts`, its entry
  and icon in `lib/nav.ts` and `NavIcon.vue`, and its styles.
- The artifacts scenes and seed data in `frontend/scripts/ui-shots/`, and any
  image only they produce.
- `docs/guide/artifacts.md`, its entry in `docs/guide/usage.md` (regenerate
  `frontend/src/data/docs.ts`), the two route rows in
  `docs/internals/api-and-transport.md`, and mentions in
  `docs/internals/architecture.md`, `docs/internals/development.md` and the
  root `README.md`.
- Any reference left in prompts or task code (`internal/handler/tasks_autoimplement.go`
  and `internal/speccomment/types.go` mention the word; check whether either
  refers to this feature).

## Acceptance

1. `GET /api/artifacts` and `GET /artifact/x.html` answer 404 in the error
   envelope on the full server.
2. The rail has no Artifacts entry, and `/artifacts` renders the not-found
   page.
3. A workspace's `artifacts/` directory and its files are unchanged after a
   start.
4. The Go suite, the frontend suite, `bunx vue-tsc --noEmit` and the
   screenshot checks in CI pass.

## Related

[live-serve](../../local/live-serve.md), building and running developed
software from within wallfacer, is the larger cousin of this feature and is
designed but not built. Its fate is a separate decision.
