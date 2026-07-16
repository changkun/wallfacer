# Artifacts

The Artifacts gallery serves self-contained web pages that live in the workspace. Slide decks, reports, and one-off visualizations produced by an agent (or written by hand) open directly in the app, without a build step or a running process. Open it from the sidebar or navigate to `/artifacts`.

## Where artifacts live

Artifacts are read from the `artifacts/` directory at the root of the first workspace folder. Any file placed there with a web content type appears in the gallery, newest first. The typical file is a single self-contained `.html` document, but referenced assets (CSS, JavaScript, images, fonts) are served from the same directory as well.

Chat and spec agents run with their working directory at the workspace root, so a request such as "create a slide deck as a single self-contained HTML file at `artifacts/deck.html`" lands the file exactly where the gallery reads it, and it appears immediately. A task agent works inside an isolated git worktree, so a task-produced artifact appears only after its branch is committed and merged.

## Opening an artifact

Selecting an artifact shows an inline preview and an **Open** action. The inline preview is a thumbnail rendered in a sandboxed frame. Full-screen viewing and keyboard controls (for a deck driven by arrow keys, for example) run in a dedicated browser tab opened with **Open**. A **Direct link** to the raw file is available for sharing the same-origin URL.

Re-rendering an artifact in place is reflected on the next load; responses are not cached.

## What is served

Only web content types are served: HTML, CSS, JavaScript, JSON, SVG, common image formats, and web fonts. Other files under `artifacts/` (source code, archives) are ignored by the gallery and are not reachable. File access is confined to the `artifacts/` directory, so paths that attempt to escape it, including symlinks, are rejected.

## Scope

The gallery serves static files only. Applications that need a build step, a long-lived process, or a bound port are outside its scope. Artifacts are served from the app's own origin and can reach the app's authenticated endpoints, which is acceptable for local single-user use.
