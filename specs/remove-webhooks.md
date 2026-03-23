# Remove Webhook Feature

Webhooks are unused infrastructure with no current consumers. Remove the entire feature to reduce maintenance surface.

## Files to Delete

- `internal/runner/webhook.go` — Core notifier, HMAC signing, retry logic
- `internal/runner/webhook_test.go` — All webhook tests

## Go Source Changes

### `internal/envconfig/envconfig.go`
- Remove `WebhookURL` and `WebhookSecret` fields from `Config` struct
- Remove `"WALLFACER_WEBHOOK_URL"` and `"WALLFACER_WEBHOOK_SECRET"` from `knownKeys`
- Remove their `case` branches in `Parse`
- Remove webhook pointer fields from `Updates` struct
- Remove their mapping in `Update`

### `internal/handler/env.go`
- Remove `WebhookURL` field from `envConfigResponse` struct
- Remove webhook masking logic in `GetEnvConfig`
- Delete `TestWebhook` handler method entirely
- Remove `webhook_url` and `webhook_secret` from `UpdateEnvConfig` request struct
- Remove webhook guard logic (empty string preservation) in update handler
- Remove webhook fields from `envconfig.Update` call

### `internal/handler/env_test.go`
- Delete `TestGetEnvConfig_WebhookURLMasked`
- Delete `TestTestWebhook_MissingConfiguration`
- Delete `TestTestWebhook_Success`
- Delete `TestTestWebhook_DownstreamFailure`

### `internal/handler/handler.go`
- Remove `webhookNotifier` field from `Handler` struct
- Remove its initialization in `NewHandler`

### `internal/cli/server.go`
- Remove webhook notifier creation and `Start()` call (~lines 176-181)
- Remove `wn.Wait()` drain in shutdown sequence (~lines 349-352)
- Remove `"TestWebhook"` entry from handlers map (~line 607)
- Remove `"TestWebhook"` entry from body-limit config (~line 704)
- Remove `"TestWebhook"` from `requiresStore` exemption list (~line 882)

### `internal/apicontract/routes.go`
- Remove `POST /api/env/test-webhook → TestWebhook` route definition
- Run `make api-contract` to regenerate `ui/js/generated/routes.js` and `docs/internals/api-contract.json`

## Frontend Changes

### `ui/partials/settings-tab-sandbox.html`
- Delete the entire "Webhook Notifications" section (~lines 649-738)

### `ui/js/envconfig.js`
- Remove `webhook_url` and `webhook_secret` from `buildSaveEnvPayload`
- Delete `testWebhookConfig` function
- Remove webhook field reset/population logic in `loadEnvConfig`
- Remove webhook secret clearing after save

## Documentation Changes

### `CLAUDE.md`
- Remove `POST /api/env/test-webhook` from API routes list
- Remove `WALLFACER_WEBHOOK_URL` and `WALLFACER_WEBHOOK_SECRET` from env var list
- Remove webhook mention from Key Conventions

### `AGENTS.md`
- Same removals as CLAUDE.md (routes, env vars, conventions)

### `README.md`
- Remove "webhook notifications" from feature list
- Remove webhook mentions from documentation index

### `docs/guide/configuration.md`
- Delete "#### Webhooks" env var table (~lines 210-215)
- Delete "### Webhooks" advanced section (~lines 368-398)

### `docs/guide/usage.md`
- Remove "webhooks" from advanced section description (~line 73)

### `docs/guide/getting-started.md`
- Remove "webhooks" from config reference mentions (~lines 97, 128)

### `docs/internals/api-and-transport.md`
- Remove `POST /api/env/test-webhook` from route table (~line 39)
- Remove `TestWebhook` from RequireStoreMiddleware exemption list (~line 150)
- Delete entire "Webhook Notifications" section (~lines 239-310)
- Remove `Webhook` participant and `wn.Wait()` from shutdown sequence diagram (~lines 515-529)
- Remove webhook drain bullet from shutdown narrative (~line 546)

### `docs/internals/api-contract.json`
- Regenerated automatically by `make api-contract`

### `docs/internals/automation.md`
- Remove "Webhook Notifier" subgraph from mermaid diagram (~lines 55-56)

### `docs/internals/orchestration.md`
- Remove "webhooks" from API & Transport cross-reference (~line 5)

### `docs/internals/architecture.md`
- Remove `WebhookNotifier` participant from sequence diagram (~line 257)
- Remove `WebhookNotifier` from runner package table (~line 309)
- Remove `POST /api/env/test-webhook` from env.go handler table (~line 327)
- Remove "webhook notifications" from Observability description (~line 381)
- Remove "webhooks" from API & Transport cross-reference (~line 396)

### `docs/internals/workspaces-and-config.md`
- Remove Webhooks row from config table (~line 181)
- Remove "webhooks" from API & Transport cross-reference (~line 304)

### `docs/internals/data-and-storage.md`
- Remove "webhook delivery" from API & Transport cross-reference (~line 561)

### `docs/internals/internals.md`
- Remove "webhook notifications" from API & Transport description (~line 35)

### `docs/internals/git-worktrees.md`
- Remove "webhooks" from API & Transport cross-reference (~line 364)

### `docs/internals/task-lifecycle.md`
- Remove "webhooks" from API & Transport cross-reference (~line 471)

### `specs/oversight-risk-scoring.md`
- Remove webhook notifier mention (~line 611)

## Execution Order

1. Delete `internal/runner/webhook.go` and `internal/runner/webhook_test.go`
2. Remove webhook fields from `internal/envconfig/envconfig.go`
3. Remove webhook handler and tests from `internal/handler/`
4. Remove webhook wiring from `internal/handler/handler.go`
5. Remove webhook startup/shutdown from `internal/cli/server.go`
6. Remove route from `internal/apicontract/routes.go`
7. Run `make api-contract` to regenerate route artifacts
8. Update frontend HTML and JS
9. Update all documentation files
10. Run `make fmt && make lint && make test` to verify clean removal
