---
title: "Shared Cella Go Client"
status: drafted
depends_on:
  - specs/cloud/latere-integration.md
affects:
  - internal/sandbox/
effort: medium
created: 2026-06-01
updated: 2026-06-01
author: changkun
dispatched_task_id: null
---

# Shared Cella Go Client

## Problem

Three Latere products converge on Cella's `/v1/sandboxes` REST API, but at
different layers and with different control flows:

| Product | Where the agent runs | Sandbox role | Natural API shape |
|---------|----------------------|--------------|-------------------|
| **Topos** (`latere.ai/x/agents`) | harness **outside**, in `toposd` | dumb workspace the harness pokes at | many small `Exec` / `ReadFile` / `WriteFile` against one long-lived sandbox |
| **Wallfacer** (`latere.ai/x/wallfacer`) | harness **inside** — the agent CLI runs in the sandbox | the agent's whole world | one `Launch` per turn, stream stdout until the CLI exits |
| **Cella** (`latere.ai/x/sandbox`) | n/a — it *is* the sandbox platform | runtime owner | ships OpenAPI; no Go client today |

Today, the **only Go client** for Cella's REST API is buried inside Topos at
`agents/internal/sandbox/cella/provider.go` — request/response DTOs, error
envelope decoding, retry, auth header wiring, and the `SandboxProvider`
abstraction are all in one package. When [cella-runtime.md](cella-runtime.md)
lands, Wallfacer's `CellaBackend` will need the same wire-level functionality.
The two paths are:

- **Duplicate the client in Wallfacer** — fast today, two copies drift apart as
  the Cella API evolves, OpenAPI bumps land in one client first.
- **Wallfacer imports Topos's `SandboxProvider`** — reuses the client, but
  forces Wallfacer through an abstraction shaped for harness-outside (many
  `Exec` calls against a workspace) when its actual model is harness-inside
  (one streamed process per launch). The fit is workable but awkward, and it
  couples Wallfacer's release cadence to Topos's.

Neither is right. The duplication risk is at the **wire layer**, not the
abstraction layer.

## Goal

Extract the Cella wire client into a standalone, importable Go package owned by
the Cella repo. Both Topos's `SandboxProvider` and Wallfacer's `CellaBackend`
become thin adapters over this shared client; neither needs to know about the
other.

## Why not unify at the abstraction layer

Topos's `SandboxProvider` and Wallfacer's `sandbox.Backend` model different
control flows:

- `SandboxProvider`: `Create → many Exec / ReadFile / WriteFile → Destroy`.
  Long-lived workspace, harness drives it externally.
- `Backend`: `Launch → stream until exit`. One process, lifetime ≈ one turn,
  harness *is* the process inside.

Forcing them into one interface makes one or both awkward — `Backend.Launch`
returning a `Handle` is a poor shape for `ReadFile`; `Provider.StreamExec` is
extra ceremony for "fork a process, stream it." The split is fundamental and
healthy. **They should share only what they genuinely share: the wire
contract.**

This also resolves the question of whether Wallfacer's `LocalBackend` (podman)
and Topos's `local.Provider` (temp-dir + `os/exec`) should unify: they
shouldn't. They serve different products and different control flows; they only
look similar by name.

## Design

### Layered picture

```
┌──────────────────────────────────────────────────────────────┐
│  Topos (harness OUTSIDE)            Wallfacer (harness INSIDE)│
│  sandbox.SandboxProvider            sandbox.Backend           │
│  ├─ local.Provider (temp-dir fake)  ├─ LocalBackend (podman)  │
│  └─ cella.Provider ─┐               ├─ HostBackend (host fork)│
│                     │               └─ CellaBackend ─┐        │
│                     ▼                                ▼        │
│              ┌──────────────────────────────────────────┐     │
│              │  latere.ai/x/sandbox/client  ◄── SHARED  │     │
│              │  (Cella REST DTOs + HTTP client)         │     │
│              └──────────────────────────────────────────┘     │
│                                │                              │
│                                ▼                              │
│                       Cella API (`/v1/sandboxes/...`)         │
└──────────────────────────────────────────────────────────────┘
```

### Module location

`latere.ai/x/sandbox/client` — published from the Cella repo, alongside
`api/openapi.yaml`. Cella owns its wire contract; the client co-evolves with
the OpenAPI spec it implements. Versioned with Cella's API tags.

Rejected alternatives:
- **Separate repo `latere.ai/x/sandbox-client`** — cleaner ownership boundary,
  but extra release machinery for no extra clarity. The client is a derivative
  of the OpenAPI; they belong together.
- **Promote `latere.ai/x/agents/sandbox/cella`** (Wallfacer imports Topos) —
  fast, but couples Wallfacer's release cadence to Topos and inverts the
  ownership story (Topos becomes Cella's de-facto Go client maintainer). OK as
  a stopgap, not a destination.

### Package surface

What the package exports:

- **DTOs** for every request/response in `/v1/sandboxes/*` — `CreateSandboxReq`,
  `Sandbox`, `CreateCommandReq`, `Command`, `LogsResp`, file ops, list filters.
- **Client struct** with one function per endpoint, returning DTOs.
- **Error contract** — `ErrNotFound`, `ErrConflict`, `*APIError` (status, code,
  message, request_id). Lifted from Topos's existing design at
  `agents/internal/sandbox/provider.go:23`; it's already the right shape.
- **Auth hook** — caller-supplied `func() (token string, err error)` so the
  package stays neutral about how tokens are minted (Topos uses its trust
  plane; Wallfacer uses the local-mode device-code token).
- **Streaming helper** for cursor-mode log polling, exposed as an
  `io.ReadCloser` over the combined byte stream. No interpretation of stream
  separation — that's a consumer choice (see open questions).
- **Pluggable `http.Client`** so callers control retry/timeout/transport.

What the package **does not** export:
- No `SandboxProvider` interface. No `Backend` interface. No `local.Provider`
  fake. No notion of "workspace" beyond what the API exposes. No control-flow
  opinion.

### Migration plan

1. **Lift the client into Cella.** Copy `agents/internal/sandbox/cella/`
   (`provider.go`, `tokensource.go`, error decoding, integration test scaffold)
   into `latere.ai/x/sandbox/client`. Strip the `SandboxProvider` adapter
   layer; keep only DTOs + raw client + error contract. Tag a `v0.1.0`.
2. **Migrate Topos.** `cella.Provider` becomes ~50 LoC of adapter:
   `SandboxProvider` methods call `client.*` functions and map errors through.
   Topos's existing test suite is the regression gate; expect a no-op diff at
   the test-output level.
3. **Implement Wallfacer's `CellaBackend`.** Per
   [cella-runtime.md](cella-runtime.md): `Launch` = `client.CreateSandbox` +
   `client.StreamCommand`; `Wait` returns the command exit code; `Kill` =
   `client.DeleteSandbox`; `List` filters by a `kind=wallfacer` label.
   `CellaBackend` is harness-inside: one launch per turn, one streamed command
   per launch.

The three steps are independently reviewable. Step 1 has zero behavior change
for any product; step 2 has zero behavior change for Topos; step 3 is the new
capability for Wallfacer.

### Stream separation policy

Cella's current API merges stdout and stderr into a single combined byte stream
in arrival order. The client surfaces what the API returns — it does **not**
synthesize per-stream separation. Each consumer decides how to map this onto
its own interface:

- Topos already accepts the merge: `ExecResult.Stdout` carries combined output,
  `Stderr` is nil (`agents/internal/sandbox/provider.go:116`).
- Wallfacer's `CellaBackend` accepts the merge for v0: `Handle.Stdout()` is the
  combined stream, `Handle.Stderr()` returns an immediately-EOF reader.
  Stream-json output is on stdout regardless, so end-to-end works; stderr-only
  diagnostics are unavailable on the Cella path until the API exposes
  separation.

Server-side stream separation is a Cella concern, tracked separately. The
client package will surface separated streams the moment the API does.

## Acceptance criteria

- `latere.ai/x/sandbox/client` exists in the Cella repo, exports DTOs +
  function-style client + error contract, has no dependency on Topos or
  Wallfacer.
- A `make test` in the Cella repo covers the client with table-driven unit
  tests against an `httptest.Server` and an opt-in integration test against a
  real Cella endpoint.
- Topos's `agents/internal/sandbox/cella/` is reduced to a `SandboxProvider`
  adapter; the old DTOs and HTTP wiring are deleted; Topos's existing tests
  pass unchanged.
- Wallfacer's `CellaBackend` (delivered by [cella-runtime.md](cella-runtime.md))
  imports `latere.ai/x/sandbox/client` and does not duplicate any wire-level
  code.
- Bumping Cella's OpenAPI version requires updating exactly one package
  (`latere.ai/x/sandbox/client`); both consumers pick it up via go.mod.

## Boundaries

- The client package owns the **wire contract** and nothing else: no workspace
  semantics, no exec semantics, no stream-separation synthesis, no opinion
  about long-lived vs ephemeral sandbox usage.
- Topos keeps `SandboxProvider` and its `local.Provider` fake; Wallfacer keeps
  `sandbox.Backend`, `LocalBackend`, and `HostBackend`. Neither product's
  abstraction layer changes shape.
- This spec does **not** specify Wallfacer's `LaunchSpec` redesign (the
  rename and field cleanup discussed in design review); that's part of
  [cella-runtime.md](cella-runtime.md) and stays local to Wallfacer.

## Open questions

- **Auth abstraction shape.** Function-style `func() (token, error)` is
  simplest. Topos may need a richer hook (refresh, scope-bound minting). Start
  simple; widen if needed.
- **Retry policy.** Idempotent endpoints (`GET`, `DELETE`) are safe to retry;
  `POST /commands` is not. Does the client embed a default policy, or stay
  retry-free and leave it to callers?
- **Versioning cadence.** Does the client tag track Cella's OpenAPI version
  (e.g. `v1.3.0` matches API `v1.3`), or its own semver? Recommend pinning to
  OpenAPI minor version for clarity.
- **Local-dev story for Wallfacer.** Topos's `local.Provider` is a dev fake at
  the `SandboxProvider` layer. Wallfacer has no equivalent below
  `CellaBackend` — for cella-mode dev without a real Cella, callers fall back
  to `--backend local` or `--backend host`. Acceptable; revisit if cella-mode
  development becomes a frequent loop.
