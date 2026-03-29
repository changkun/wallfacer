# Task 6: Desktop Browser Launch Integration

**Status:** Done
**Depends on:** 5
**Phase:** Desktop integration
**Effort:** Small

## Goal

When running as a desktop app (Wails), OAuth authorization URLs must open in the system's default browser, not the embedded WebView. OAuth providers may block embedded WebViews.

## What to do

1. In `ui/js/envconfig.js`, update the `startOAuthFlow` function's URL-opening logic:
   - Check if `window.runtime?.BrowserOpenURL` is available (Wails runtime).
   - If yes: call `window.runtime.BrowserOpenURL(authorizeURL)`.
   - If no (browser mode): call `window.open(authorizeURL, "_blank")`.

2. This is a small conditional in the existing `startOAuthFlow` function added in task 5. The Wails runtime JS bindings are already generated at `ui/wailsjs/runtime/runtime.js` and expose `BrowserOpenURL`.

## Tests

- `test("desktop mode uses BrowserOpenURL")` — mock `window.runtime.BrowserOpenURL`, call `startOAuthFlow`, verify it was called instead of `window.open`.
- `test("browser mode uses window.open")` — ensure `window.runtime` is undefined, call `startOAuthFlow`, verify `window.open` was called.

## Boundaries

- Do NOT modify Go backend code (`internal/cli/desktop.go`) — the browser opening is handled client-side via the Wails JS runtime.
- Do NOT change the OAuth flow logic itself.
