# Task 2: Container Selector UI

**Status:** Done
**Depends on:** Task 1
**Phase:** Frontend
**Effort:** Medium

## Goal

Add a container selector dropdown to the terminal tab bar so users can open a shell inside a running task container directly from the terminal panel.

## What to do

1. In `ui/partials/status-bar.html`, add a container selector button next to the "+" button in `#terminal-tab-bar`:
   ```html
   <button id="terminal-container-btn" class="terminal-tab-add" title="Attach to container" aria-label="Attach to running container" tabindex="-1">&#9654;</button>
   ```
   Use a simple play/arrow icon or container icon to distinguish from the "+" (new host shell) button.

2. In `ui/js/terminal.js`, add a container picker function:
   - `_showContainerPicker()` — fetches `GET /api/containers`, builds a dropdown/popover listing running containers with their task titles.
   - Each item shows: task title + short container ID (e.g., "fix-auth @ 3b616d1e").
   - Clicking an item sends `{"type":"create_session","container":"<container-name>"}`.
   - If no containers are running, show a "No running containers" message.

3. Wire the container button's click handler to `_showContainerPicker()`. Apply `mousedown preventDefault` to prevent focus theft (same pattern as the "+" button).

4. Update `addTerminalTab` to accept an optional label parameter from the `sessions` list. When the server's `sessions` response includes a `container` field, use a label like "Container: <short-name>" instead of "Shell N".

5. Update `_handleSessionsList` to pass the container info as the tab label when creating tabs for container sessions.

6. Style the container picker dropdown:
   - Position it above the button (since the terminal is at the bottom of the screen).
   - Match the existing UI aesthetic (use CSS variables, compact items).
   - Dismiss on click outside or Escape.

## Tests

- `TestContainerPicker_FetchAndDisplay` — mock `GET /api/containers` response, verify dropdown items created with correct task titles.
- `TestContainerPicker_SelectSendsMessage` — click a container item, verify `create_session` message sent with correct container name.
- `TestContainerPicker_EmptyState` — mock empty containers response, verify "No running containers" message.
- `TestContainerPicker_DismissOnEscape` — verify picker closes on Escape key.
- `TestContainerTab_Label` — verify container sessions get "Container: ..." labels instead of "Shell N".
- Run `make test-frontend` to verify existing tests still pass.

## Boundaries

- Do NOT change the backend (that's Task 1).
- Do NOT add container lifecycle management (start/stop containers from the picker).
- Do NOT add container log viewing — this is exec-only.
- Keep the picker simple — a flat list, no filtering or search.
