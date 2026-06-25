---
title: Env Test Reauth Flag
status: archived
depends_on: []
affects: []
effort: small
created: 2026-03-28
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---


# Task 7: Env Test Reauth Flag

## Goal

When `POST /api/env/test` detects an authentication error (401/403, "invalid token", "expired token"), include a `reauth_available: true` flag in the response so the frontend knows to offer a "Sign in again" action.

## What to do

1. In `internal/handler/env.go`, add a `ReauthAvailable bool` field to `sandboxTestResponse`:
   ```go
   ReauthAvailable bool `json:"reauth_available,omitempty"`
   ```

2. In the `TestSandbox` handler, after the test run completes, inspect the task result for auth failure indicators:
   - Check `task.LastTestResult` and `task.Result` for patterns: `"unauthorized"`, `"401"`, `"403"`, `"invalid.*token"`, `"expired.*token"`, `"authentication"` (case-insensitive).
   - Check that the provider supports OAuth sign-in: Claude always does; Codex always does. If a custom base URL is configured, set `ReauthAvailable = false` (custom endpoints won't use standard OAuth).

3. Set `resp.ReauthAvailable = true` when an auth error is detected and OAuth is available for that provider.

4. Read the current base URL from the env config to determine if a custom URL is set: if `ANTHROPIC_BASE_URL` is non-empty (for Claude) or `OPENAI_BASE_URL` is non-empty (for Codex), OAuth is not available.

## Tests

- `TestTestSandbox_ReauthAvailableOnAuthError` — mock a test that results in a "401 unauthorized" output, verify `reauth_available: true` in response.
- `TestTestSandbox_NoReauthWhenCustomBaseURL` — set a custom base URL, trigger an auth error, verify `reauth_available: false`.
- `TestTestSandbox_NoReauthOnNonAuthError` — trigger a non-auth error (e.g., timeout), verify `reauth_available` is false/omitted.

## Boundaries

- Do NOT implement the UI for re-auth prompts (that's task 8).
- Do NOT modify the OAuth flow engine or auth handlers.
- Keep the detection heuristic simple — exact substring matches, not complex NLP.
