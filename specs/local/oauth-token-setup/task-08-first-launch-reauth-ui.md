# Task 8: First-Launch Hints and Re-Auth UI

**Status:** Done
**Depends on:** 5, 7
**Phase:** Frontend polish
**Effort:** Medium

## Goal

Guide users toward OAuth sign-in when no credentials are configured (first launch) and when existing tokens are invalid or expired (re-authentication). These are UI enhancements that build on the sign-in buttons from task 5 and the reauth flag from task 7.

## What to do

### First-launch hints

1. In `ui/js/envconfig.js`, in `loadEnvConfig()`, after loading the env config response:
   - Check if neither `oauth_token` nor `api_key` has a placeholder value (both "(not set)") for Claude.
   - Check if `openai_api_key` has no placeholder value for Codex.
   - If no credentials are configured for a provider, visually emphasize its "Sign in" button (e.g., add a CSS class like `btn-primary` instead of `btn-secondary`).

2. In `ui/partials/api-config-modal.html`, add a hint text element (initially hidden):
   - `id="claude-no-creds-hint"`: "No token configured — sign in to get started"
   - `id="codex-no-creds-hint"`: similar.
   - Show these when no credentials are detected; hide when credentials exist.

3. On first app launch with no credentials for any provider, show a toast notification: "Set up your API credentials to get started" with a link/button that opens the Settings panel. Detect "first launch" by checking if no credentials exist when the page loads. Use the existing toast/notification pattern in the UI.

### Invalid token re-auth

4. In the sandbox test result handler (`testSandboxConfig` in `envconfig.js`), check for `reauth_available: true` in the response:
   - If true: show an inline warning next to the test status: "Token invalid or expired" with a "Sign in again" button.
   - The "Sign in again" button calls the existing `startOAuthFlow(provider)`.

5. After a successful re-authentication (OAuth flow completes for a provider that had an auth error), clear any inline warning and refresh the env config display.

## Tests

- `test("first-launch emphasis when no Claude credentials")` — mock env response with empty tokens, verify sign-in button has primary style and hint is visible.
- `test("first-launch emphasis removed when credentials exist")` — mock env response with a masked token, verify button is default style and hint is hidden.
- `test("reauth warning shown on auth error with reauth_available")` — mock test response with `reauth_available: true`, verify warning and "Sign in again" button appear.
- `test("reauth warning hidden when reauth_available is false")` — mock test response without the flag, verify no warning.
- `test("toast shown on first launch with no credentials")` — mock env with no tokens, verify toast notification appears.

## Boundaries

- Do NOT modify Go backend code in this task.
- Do NOT change the OAuth flow logic — only add UI affordances around it.
- Do NOT implement auto-retry of failed tasks after re-auth (out of scope per spec).
