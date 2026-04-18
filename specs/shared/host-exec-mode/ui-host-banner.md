---
title: Settings UI shows host-mode isolation warning
status: validated
depends_on:
  - specs/shared/host-exec-mode/runner-host-switch.md
affects:
  - ui/partials/settings-tab-sandbox.html
  - ui/js/images.js
  - internal/handler/config.go
effort: small
created: 2026-04-18
updated: 2026-04-18
author: changkun
dispatched_task_id: null
---

# Settings UI shows host-mode isolation warning

## Goal

Make it obvious that host mode removes filesystem isolation. When the server is running with `WALLFACER_SANDBOX_BACKEND=host`, the Settings page shows a persistent warning banner so users can't accidentally be unaware that tasks have full access to their home directory.

## What to do

1. In `internal/handler/config.go` (`GET /api/config` handler), expose the runner's host-mode state to the frontend:
   - Add a `HostMode bool` field to the JSON response struct (grep for where `Workspaces`, `AutopilotFlags` are serialized today).
   - Populate it from `r.HostMode()`.

2. In `ui/partials/settings-tab-sandbox.html`, add a new alert at the top of the tab (before the `Container Images` card). Use the project's existing alert styling — grep `class=".*alert` or similar in `ui/partials/` / `ui/css/` to find the pattern. The markup skeleton:

   ```html
   <div class="settings-card alert-warn" data-js-host-mode-banner hidden>
     <strong>Host mode active.</strong>
     Tasks run directly on your machine with your user's permissions.
     Wallfacer cannot prevent an agent from writing outside the worktree.
     Recommended only on trusted machines.
   </div>
   ```

3. In `ui/js/images.js` (which already renders the Sandbox tab's container-images list), after the existing config fetch resolves, toggle the banner:

   ```js
   const banner = document.querySelector("[data-js-host-mode-banner]");
   if (banner && cfg.host_mode) banner.hidden = false;
   ```

   If `images.js` does not already read `/api/config`, add a single fetch (reuse the existing `api.js` helper) — do not duplicate fetches elsewhere.

## Tests

- Vitest unit test co-located with `ui/js/tests/settings-layout.test.js` (or a new `images.test.js` alongside `images.js` following the project's test layout): stub `fetch('/api/config')` returning `{ host_mode: true, ... }`; mount the sandbox tab partial HTML via `jsdom`; assert the banner's `hidden` attribute is removed. Add a second case for `host_mode: false` where the banner remains hidden.
- Backend unit test in `internal/handler/config_test.go`: with a `MockRunner` returning `HostMode() == true`, hit `GET /api/config`; assert the JSON body contains `"host_mode":true`.

## Boundaries

- Do **not** block settings changes in host mode — the banner is purely informational.
- Do **not** add a toggle to switch backends from the UI — backend selection requires a restart.
- Do **not** change banner copy to reference specific exploits or scary scenarios; keep it factual and brief.
- Do **not** add the banner to pages other than Settings in this task — if we want a persistent header badge, that is a follow-up.
