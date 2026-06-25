---
title: UI Sign-In Buttons and Polling
status: archived
depends_on:
  - specs/local/oauth-token-setup/task-04-auth-handler-routes.md
affects: []
effort: medium
created: 2026-03-28
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---


# Task 5: UI Sign-In Buttons and Polling

## Goal

Add "Sign in" buttons to the Settings API Configuration panel that trigger OAuth flows and poll for completion. When the token is received, the UI refreshes to show the masked token.

## What to do

1. In `ui/partials/api-config-modal.html`, add a "Sign in" button next to the OAuth Token input for Claude:
   - Button text: "Sign in" with a small icon or just text.
   - `id="claude-oauth-signin-btn"`.
   - While a flow is pending, show a spinner and "Waiting for browser..." text; disable the button.
   - Hide the button when a custom `ANTHROPIC_BASE_URL` is set (non-empty base URL input).

2. Add a similar "Sign in" button next to the OpenAI API Key input for Codex:
   - `id="codex-oauth-signin-btn"`.
   - Hide when a custom `OPENAI_BASE_URL` is set.

3. In `ui/js/envconfig.js`, add:
   - `startOAuthFlow(provider)` function:
     - POST to `Routes.auth.start({provider})`.
     - Open the returned `authorize_url` via `window.open()` (or `window.runtime?.BrowserOpenURL()` if available for desktop — see task 6).
     - Start polling `Routes.auth.status({provider})` every 2 seconds.
     - On `"success"`: stop polling, reload env config to refresh token display, show a success toast.
     - On `"error"`: stop polling, show error message inline.
     - On timeout (no success after 5 min): stop polling, show timeout message.
   - Wire the sign-in buttons' `onclick` to `startOAuthFlow("claude")` / `startOAuthFlow("codex")`.
   - `cancelOAuthFlow(provider)` function: POST to cancel endpoint, reset button state.

4. Update the sign-in button visibility: in `loadEnvConfig()`, after loading the config, check if base URLs are custom and hide/show sign-in buttons accordingly. Also add `input` event listeners on the base URL fields to toggle button visibility dynamically.

5. Run `make api-contract` if not already done (routes added in task 4).

## Tests

Frontend tests in `ui/js/__tests__/`:

- `test("sign-in button hidden when custom base URL set")` — set base URL input value, trigger visibility check, verify button is hidden.
- `test("sign-in button visible when base URL empty")` — clear base URL, verify button visible.
- `test("startOAuthFlow calls start endpoint and opens URL")` — mock `api()` and `window.open`, call `startOAuthFlow`, verify both called.
- `test("polling stops on success")` — mock status endpoint returning `{state:"success"}`, verify polling clears interval.
- `test("polling stops on error")` — mock status endpoint returning `{state:"error"}`, verify polling stops and error displayed.

## Boundaries

- Do NOT implement first-launch emphasis or auto-open Settings (that's task 8).
- Do NOT implement re-auth warnings for invalid tokens (that's task 8).
- Do NOT modify Go backend code in this task.
