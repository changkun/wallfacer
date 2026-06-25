---
title: Auth HTTP Handlers and Route Registration
status: archived
depends_on:
  - specs/local/oauth-token-setup/task-03-flow-engine.md
affects: []
effort: medium
created: 2026-03-28
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---


# Task 4: Auth HTTP Handlers and Route Registration

## Goal

Expose the OAuth flow engine via HTTP endpoints on the Wallfacer server. Register three new routes per provider and wire the `oauth.Manager` into the handler.

## What to do

1. Create `internal/handler/auth.go` with methods on `*Handler`:
   - `StartOAuth(w http.ResponseWriter, r *http.Request)`:
     - Extract `{provider}` from the URL path (use `r.PathValue("provider")`).
     - Map `"claude"` to `oauth.ClaudeProvider`, `"codex"` to `oauth.CodexProvider`. Return 400 for unknown.
     - Call `h.oauthManager.Start(r.Context(), provider)`.
     - Return `{"authorize_url": "..."}` as JSON.
   - `OAuthStatus(w http.ResponseWriter, r *http.Request)`:
     - Extract provider, call `h.oauthManager.Status(providerName)`.
     - Return `{"state": "pending"|"success"|"error", "error": "..."}`.
   - `CancelOAuth(w http.ResponseWriter, r *http.Request)`:
     - Extract provider, call `h.oauthManager.Cancel(providerName)`.
     - Return 204 No Content.

2. Add an `oauthManager *oauth.Manager` field to the `Handler` struct in `internal/handler/handler.go`. Initialize it in `NewHandler`:
   - `oauthManager: oauth.NewManager()` with `TokenWriter` set to a closure that calls `envconfig.Update(h.envFile, ...)` with the appropriate key.

3. Add routes to `internal/apicontract/routes.go`:
   ```
   POST /api/auth/{provider}/start   → StartOAuth
   GET  /api/auth/{provider}/status  → OAuthStatus
   POST /api/auth/{provider}/cancel  → CancelOAuth
   ```
   Use tag `"auth"`. Run `make api-contract` to regenerate JS routes.

## Tests

- `TestStartOAuth_UnknownProvider` — POST with provider `"llama"`, expect 400.
- `TestStartOAuth_ReturnsAuthorizeURL` — POST with provider `"claude"`, verify response contains `authorize_url` with expected host.
- `TestOAuthStatus_NoActiveFlow` — GET status with no flow started, verify appropriate response (error or empty state).
- `TestCancelOAuth_NoActiveFlow` — POST cancel with no flow, verify 204 (idempotent).
- `TestStartOAuth_Integration` — start a flow, verify status is pending, cancel it, verify status changes.

## Boundaries

- Do NOT implement the UI changes (that's task 5).
- Do NOT add desktop-specific browser launch logic (that's task 6).
- Do NOT modify `env.go` for reauth (that's task 7).
