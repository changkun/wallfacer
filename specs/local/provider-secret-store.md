---
title: Provider credentials in the system keyring
status: drafted
depends_on: []
affects:
  - internal/envconfig/
  - internal/executor/
  - internal/handler/env.go
  - internal/cli/
  - frontend/src/components/settings/SettingsTabSandbox.vue
effort: medium
created: 2026-09-13
updated: 2026-09-13
author: changkun
dispatched_task_id: null
---

# Provider credentials in the system keyring

## Overview

Settings can move provider credentials out of the plaintext configuration file
into the operating system keyring. Existing file-based and headless deployments
remain supported. Enabling keyring storage is explicit and never falls back to
plaintext when the keyring is unavailable.

## Current state

`envconfig.Update` writes credentials directly into `.env`. `Parse`, `ReadRaw`,
host execution, and native Topos consume those values. Settings smoke tests copy
the file and apply temporary overrides. OAuth completion also calls `Update`.

## Design

- `WALLFACER_SECRET_STORE=keyring` and a UUID bundle reference identify a provider
  credential bundle under the `wallfacer` keyring service. `zalando/go-keyring`
  supplies native macOS, Windows, and Linux implementations without cgo.
- Enabling storage migrates Claude OAuth, Anthropic API/auth tokens, OpenAI,
  Cursor, and OpenCode credentials. Server authentication remains separate.
- Stage and verify a new bundle before atomically replacing the file. Remove
  all active plaintext credential entries, including duplicates and exported
  entries. Preserve ordinary settings and comments. Failed writes retain the
  prior configuration. Concurrent updates are serialized.
- All credential reads resolve the bundle through one shared path. Missing or
  locked secrets fail explicitly; host launches cannot use unrelated inherited
  credentials after a resolution failure. Startup cannot change cloud mode
  because secret resolution failed.
- Settings test overrides use an independent temporary bundle and remove it
  after the test; saved credentials stay unchanged.
- Settings exposes the choice through `secret_store` on GET/PUT `/api/env`.
  Switching back to file storage requires an explicit selection.

## Acceptance and verification

- Migration removes every provider token from `.env`; both readers return the
  same original values, and Settings GET remains masked.
- Save, read, and file-write failures never cause plaintext fallback or erase
  working credentials. Concurrent updates preserve unrelated fields.
- Smoke tests and OAuth use the same storage choice; test-only credentials and
  temporary references are cleaned up.
- A fake host harness receives resolved credentials; inaccessible references
  prevent launch. Configuration tests preserve cloud authentication on error.
- UI tests cover choosing keyring storage and receiving a visible failure.

Related acquisition and GitHub-token specs retain their existing ownership;
neither is a prerequisite for this local provider-storage feature.
