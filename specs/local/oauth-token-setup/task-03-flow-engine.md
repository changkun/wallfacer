# Task 3: OAuth Flow Engine and Provider Configs

**Status:** Done
**Depends on:** 1, 2
**Phase:** Core OAuth infrastructure
**Effort:** Medium

## Goal

Implement the OAuth flow orchestration that ties PKCE, callback server, and token exchange together. Define provider-specific configurations for Claude and Codex. The flow engine manages the lifecycle of one OAuth attempt per provider: start, poll status, cancel.

## What to do

1. Create `internal/oauth/provider.go` with:
   - `Provider` struct: `Name string`, `AuthorizeURL string`, `TokenURL string`, `ClientID string`, `Scopes []string`, `TokenEnvKey string` (the env var name to write, e.g. `CLAUDE_CODE_OAUTH_TOKEN`).
   - `var ClaudeProvider` and `var CodexProvider` package-level variables with the correct URLs and client IDs from the spec.

2. Create `internal/oauth/flow.go` with:
   - `FlowState` enum: `Pending`, `Success`, `Error`.
   - `FlowStatus` struct: `State FlowState`, `Error string`.
   - `Flow` struct holding: provider, PKCE verifier/challenge, state param, callback server, result token, status, and a mutex.
   - `Manager` struct: holds a `map[string]*Flow` (keyed by provider name) and a mutex. Only one flow per provider at a time.
   - `NewManager() *Manager`.
   - `(m *Manager) Start(ctx context.Context, provider Provider) (authorizeURL string, err error)`:
     - Cancel any existing flow for this provider.
     - Generate PKCE verifier + challenge + state.
     - Start a `CallbackServer` with 5-minute timeout.
     - Build the authorization URL with query params: `client_id`, `redirect_uri` (using callback port), `response_type=code`, `code_challenge`, `code_challenge_method=S256`, `state`, and provider scopes if any.
     - Store the flow in the map.
     - Launch a goroutine that: waits for the callback, validates state, exchanges the code for a token, updates flow status.
     - Return the authorization URL.
   - `(m *Manager) Status(providerName string) FlowStatus`.
   - `(m *Manager) Cancel(providerName string)`.

3. Implement `exchangeToken(ctx context.Context, provider Provider, code, verifier, redirectURI string) (string, error)` in `flow.go`:
   - POST to `provider.TokenURL` with form-encoded body: `grant_type=authorization_code`, `code`, `redirect_uri`, `code_verifier`, `client_id`.
   - Parse JSON response for `access_token` (or `api_key` depending on provider).
   - Return the token string.

4. After successful token exchange, the flow engine should call a callback to write the token. Use a `TokenWriter func(envKey, token string) error` field on `Manager`, which the handler will set to call `envconfig.Update`.

## Tests

- `TestManager_StartBuildsCorrectURL` — start a flow with a mock provider, verify the returned URL contains all expected query params.
- `TestManager_StartCancelsPreviousFlow` — start two flows for the same provider, verify the first is cancelled.
- `TestManager_StatusPending` — start a flow, immediately check status, verify `Pending`.
- `TestManager_Cancel` — start a flow, cancel it, verify status is `Error` with cancellation message.
- `TestExchangeToken` — use `httptest.NewServer` as a mock token endpoint, verify correct form params are sent and token is extracted from response.
- `TestExchangeToken_ErrorResponse` — mock returns an error JSON, verify it surfaces.
- `TestManager_FullFlow` — integration: start flow with mock provider and mock token endpoint, send callback to the ephemeral server, verify status transitions to `Success` and `TokenWriter` is called with the correct env key and token value.

## Boundaries

- Do NOT create HTTP handlers or register routes (that's task 4).
- Do NOT modify any existing files yet.
- Do NOT implement token refresh.
