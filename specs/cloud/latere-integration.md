---
title: Latere Platform Integration
status: drafted
depends_on:
  - specs/identity/authentication.md
affects:
  - internal/executor/
  - internal/runner/
  - internal/agentgraph/
  - internal/auth/
  - internal/coordinator/
effort: large
created: 2026-05-30
updated: 2026-10-02
author: changkun
dispatched_task_id: null
---

# Latere Platform Integration

Umbrella for the cloud track. It states what the Latere platform is, which
parts of it wallfacer calls, and which earlier integration designs are
retired. Read it before any other spec under `specs/cloud/`.

## Problem

Wallfacer is a Latere application that runs as a self-contained local binary.
The first version of this umbrella (2026-05) planned one thin client per
Latere service, because each service was then a product of its own with its
own host: an identity provider, a sandbox runtime, a file data plane, a model
gateway, and a tool registry. Five leaf specs followed that shape, one per
service.

Latere has since consolidated those services into **one platform**:

| Surface | Address | What it is |
|---|---|---|
| Console | `https://platform.latere.ai` | One sign-in and one navigation over every capability, with the developer documentation |
| API origin | `https://api.latere.ai/v1/<capability>` | One host for every API, partitioned by capability prefix |
| Issuer | `https://auth.latere.ai` | Identity, organizations, and the one credential every capability accepts |
| Git transport | `https://code.latere.ai` | Clone and push for the Repos capability |

| Capability | Prefix | What it is |
|---|---|---|
| Agents | `/v1/agents` | Hosted agents: versioned configurations, the sessions held with them, and the triggers that start sessions |
| Models | `/v1/models` | One address for every model provider, with policy, cost and audit |
| Repos | `/v1/repos` | Git repositories |
| Environments | `/v1/environments` | Sandboxed workloads with a durable workspace |
| Storage | `/v1/storage` | Files, versions, shares and workspaces |
| Apps | `/v1/apps` | Web applications hosted from a repository |

The per-service hosts the old leaves targeted no longer answer, and the
platform composes its own capabilities: a hosted agent session opens its own
workload in Environments, clones from Repos, calls Models, and is billed
under one budget. Three of the five old leaves designed a second copy of that
composition inside wallfacer (sandbox creation, credential custody, worktree
transport). They are archived, and this umbrella replaces their premise.

## The rule: one client of the Agents capability

In cloud mode wallfacer dispatches work to **Agents** and to nothing beneath
it. Wallfacer never creates a sandbox, never stores a file in Storage on a
task's behalf, never holds a provider key for a remote run, and never moves a
worktree into a remote machine. The session does each of those for itself,
under the platform's authorization and budget.

| Capability | Called by wallfacer | For |
|---|---|---|
| Identity | yes, shipped | Sign-in, the principal (`Sub`, `OrgID`), and one actor token per service called |
| Agents | yes, drafted | Remote execution: a task runs as a hosted session ([topos-remote-executor.md](latere-integration/topos-remote-executor.md)) |
| Repos | git transport only | The remote a dispatched task's branch is pushed to and the session's branch is fetched from, with plain `git`; no API call |
| Models | yes, shipped, local runs only | An optional gateway for a local harness or the embedded agent runtime, configured as a base URL and a key |
| Environments | no | The session's workload is the platform's |
| Storage | no | Nothing in wallfacer's task or spec model is a platform file |
| Apps | no | No spec. A task whose outcome is a web app would reach Apps through its repository, not through a wallfacer call |

Why the rule holds:

- **One place composes resources.** Sandbox lifetime, credential injection at
  the workload's egress, repository delivery, model access and the budget are
  one mechanism in the platform. A second composition in wallfacer would need
  the same guarantees and a second audit trail.
- **The session owns its workspace.** A coding harness needs a git working
  tree on local disk. The session clones one and pushes a branch; nothing is
  mounted and nothing is staged, so no file plane sits between wallfacer and
  the run.
- **Local mode is unaffected.** A local harness that points at Models is a
  model client like any other. No job sits above a process on the user's
  machine, so there is nothing to bypass.

## Two axes

Cloud work has two independent axes that share only the signed-in principal.

| Axis | What | What leaves the machine | Status | Anchor |
|------|------|-------------------------|--------|--------|
| **A. Coordination plane** | Signed-in local instances hold one outbound connection to a coordinator role on wallfacer's own server. Presence, remote control, metadata projection, collaboration relay. Local stays source of truth | Allow-listed metadata, presence, and spec comments | Connection and spec comments shipped; the rest drafted | [coordination-plane.md](latere-integration/coordination-plane.md) |
| **B. Remote execution** | A task runs as a hosted agent session on the platform | The repository the task works on, deliberately and per task | Drafted, gated by demand | [topos-remote-executor.md](latere-integration/topos-remote-executor.md) |

Axis A is wallfacer's own domain (specs, tasks, board presence across a
user's own instances) and is not a platform capability. The consolidation
does not change it. Axis B is where the consolidation applies: it was two
parallel executors (a sandbox backend and an agent backend) plus a file
plane, and is now one executor over `/v1/agents`.

Two specs for dispatching to another vendor's hosted agents
([claude-managed-agents.md](../.archive/cloud/claude-managed-agents.md),
[antigravity.md](../.archive/cloud/antigravity.md)) once sat beside Axis B. They are archived
as outdated; the hosted executor is the only remote executor.

## What wallfacer owns

Wallfacer **owns** the spec model, the task lifecycle, the agent graph,
oversight, the git and worktree workflow, autonomy controls, the local-first
experience, and coordination among a user's own instances.

Wallfacer **does not own** identity, authorization policy, the hosted agent
runtime, sandbox runtime, model key custody and routing, file storage, git
hosting, budgets, or billing. Each is a platform concern reached through one
of the calls above or not reached at all.

Billing in particular has no wallfacer surface, built or planned. Wallfacer
charges for nothing and shows no plan, invoice, wallet or subscription; those
are in the platform's console. Wallfacer shows cost as a figure on a task: an
estimate from token counts for a local run, and the amount the platform
reports for a hosted one.

## Credentials

One sign-in covers everything. The login session's token is the issuer's and
is addressed to no service. For each service it calls, wallfacer mints a
short-lived **actor token** from that session, addressed to the service's
audience (`oidc.Client.ActorToken`). The coordination connector already works
this way for the coordinator's audience
(`coordinationMinter` in `internal/cli/coordination.go`). A platform call
mints for the audience `api.latere.ai`.

Consequences:

- Wallfacer adds no token-exchange endpoint and stores no platform key. A
  refresh is a new mint from the session.
- No credential stands for the user. A hosted session acts with the agent's
  own identity, bounded by what the person who started it may do; wallfacer's
  token reaches the Agents API and nothing of the session's.
- Authorization is decided by the platform for the person behind the token.
  Wallfacer decides nothing about a platform resource. It shows a refusal as
  the platform states it: the `code`, the fixed `message`, and the `details`.

## Integration seams

Each seam is config-gated. With no sign-in and no platform configuration the
seam is inert and local behavior is byte-identical to a build without it.

| Seam | Platform side | Wallfacer side | Status | Spec |
|------|---------------|----------------|--------|------|
| **Identity** | `auth.latere.ai` | `internal/auth` middleware over `authkit/jwt` and `authkit/oidc`; `authkit.Identity{Sub,OrgID}` principal | shipped | [authentication.md](../.archive/identity/authentication.md) |
| **Models for local runs** | `/v1/models` | The harness's base-URL variable and key in the env file; `agentgraph.ModelConfig` in Lux mode, with `gatewayRoot` in `internal/runner/agentic.go` deriving the gateway root from the harness-shaped URL | shipped | none needed |
| **Remote execution** | `/v1/agents` | One remote executor that runs a task as a session and maps the session's events onto the task timeline | drafted | [topos-remote-executor.md](latere-integration/topos-remote-executor.md) |
| **Coordination plane** | none; wallfacer's own server role | One outbound connection per signed-in, opted-in instance; a `store.TaskEvent` tap redacted to an allow-list | part shipped | [coordination-plane.md](latere-integration/coordination-plane.md) |
| **Hosted board** | the cluster the board is deployed into | A second workload beside the public site, with a volume for task data | drafted | [cloud-infrastructure.md](cloud-infrastructure.md) |
| **Data boundary** | none | The gates and allow-lists on every path that leaves the machine | drafted | [data-boundary-enforcement.md](data-boundary-enforcement.md) |

## Design rules

1. **Local-first is invariant.** No seam changes local-anonymous behavior.
   The default build with no sign-in runs a host agent process over a
   filesystem store with no network dependency on Latere. (True of the CLI
   harnesses that exist today. Under
   [platform-native](../shared/platform-native.md), an agent run needs a
   model credential, from a Latere sign-in or a provider sign-in; an
   instance with neither keeps everything local that needs no model. This
   rule is restated when that migration lands.)
2. **Config-gated, nil-safe selection.** A seam activates only when its
   configuration is present. Otherwise its client is nil and call sites
   short-circuit, the way `AuthProvider` and `jwtValidator` already do.
3. **One origin, addressed by capability root.** A platform call goes to a
   capability's root, by default under `https://api.latere.ai/v1/`. The
   configurable unit is the root (for example the Agents root), because a
   self-hosted core serves the same routes at the root of its own address.
   No code names a per-service host.
4. **Interface first.** A seam implements an interface wallfacer already has
   (`executor.Backend`, the `agentgraph` runner seam, `handler.AuthProvider`)
   before it introduces a new one.
5. **Data boundary holds.** Anything leaving the machine obeys
   [data-boundary-enforcement.md](data-boundary-enforcement.md): metadata may
   leave on the coordination channel; source, diffs, agent output, secrets
   and repository paths may not. Remote execution sends a repository off the
   machine on purpose, so it is opt-in per task and names what leaves before
   it leaves.
6. **One owner per concern.** Where the platform owns a concern, wallfacer
   calls it and keeps no parallel implementation.
7. **The platform's contract passes through.** Errors keep the platform's
   envelope, lists use its cursors, and every create carries an
   `Idempotency-Key`. Wallfacer does not translate them into a vocabulary of
   its own.

## Phasing

- **Phase 1, done.** Identity sign-in, JWT validation, the principal and
  organization model, per-account isolation of local projects and tasks.
- **Phase 2, in progress: Axis A.** The outbound connection and
  cloud-resident spec comments shipped. Presence, the metadata projection and
  remote control are drafted on the same connection.
- **Phase 3, drafted: Axis B.** One executor over the Agents capability. It
  is gated by demand, and by two platform conditions the executor spec
  tracks: sessions run for personal agents only until an organization's
  agents can be given a budget, and a session's push credential covers the
  platform's git host only.

## Retired by the consolidation

| Retired | Was | Now |
|---|---|---|
| [cella-runtime.md](../.archive/cloud/latere-integration/cella-runtime.md) | A `CellaBackend` creating sandboxes at a standalone sandbox host, with secrets through its vault | Not built. The session opens its own workload |
| [shared-cella-client.md](../.archive/cloud/latere-integration/shared-cella-client.md) | A sandbox wire client shared with the agent runtime | No wallfacer caller remains |
| [tenant-filesystem.md](../.archive/cloud/tenant-filesystem.md) | Staging worktrees through a file plane's workspace API | The file plane is gone; a session clones and pushes |
| [tenant-api.md](../.archive/cloud/tenant-api.md) | A wallfacer-issued API key per tenant | One platform key for every capability; wallfacer keeps no key store |
| Model keys as a future seam | Credential injection into the task environment | Shipped as plain configuration for local runs; a hosted session's model access is the platform's |
| Tool catalog as a future seam | Resolving approved tools from a registry service | No such service. A hosted agent's tools are part of its configuration |

Earlier retirements ([multi-tenant.md](../.archive/cloud/multi-tenant.md),
[billing-idempotency.md](../.archive/cloud/billing-idempotency.md)) stand for
the same reason: the concern is the platform's.

### Shipped code left without a caller

One piece of the retired design was built. The sandbox proxy
(`internal/handler/sandbox_proxy.go`, routes under
`/internal/sandbox-proxy/llm/`) lets a cloud sandbox that wallfacer created
reach a model provider without holding the provider's key: the sandbox
presents a service token for the audience `wallfacer-sandbox-proxy`, and the
proxy substitutes the key. It has no caller: nothing mints that token and no
sandbox exists to present it.

Under this umbrella nothing ever will. A hosted session's model access is its
own, through the platform, and its workload never calls wallfacer. Removing
the proxy, its routes, its audience, and the page that documents it
(`docs/internals/service-identity.md`) is a code change with its own commit
and tests. It is listed here so the removal is a decision, not an oversight.

## Boundaries

- Do **not** build sandbox scheduling, identity, file storage, model-key
  custody, git hosting, or billing inside wallfacer.
- Do **not** call a capability beneath Agents on behalf of a remote run.
- Do **not** gate or alter local execution behind any seam.
- This umbrella defines the contract and the selection rule. Each leaf
  carries its own design, tests and docs.

## Open questions

1. **Hosted board.** Whether wallfacer runs as a hosted task board, at which
   address, and under which product name is a product decision that is not
   made here. [cloud-infrastructure.md](cloud-infrastructure.md) carries the
   deployment shape so the decision has something concrete to accept or
   reject.
2. **Projection overlap.** The platform already records a hosted session's
   tokens and cost. The Axis A metadata projection carries usage for local
   runs. Whether an organization's dashboard reads both, and where it lives,
   is open in
   [metadata-projection.md](latere-integration/coordination-plane/metadata-projection.md).
3. **Repositories outside the platform's git host.** A hosted session clones
   a repository on another host without a credential, so only a public one,
   and cannot push to it. The executor spec states the v1 answer (a
   repository on the platform's git host). A repository on another host is
   out of scope until the platform offers a connector for it: wallfacer
   removed its own GitHub integration on 2026-10-02
   ([remove-github-integration](../.archive/shared/platform-native/remove-github-integration.md)).
