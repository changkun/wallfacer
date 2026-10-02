---
title: Hosted Board Deployment
status: drafted
depends_on:
  - specs/cloud/latere-integration.md
  - specs/cloud/latere-integration/topos-remote-executor.md
affects:
  - deploy/
  - Dockerfile.wallfacerd
  - .github/workflows/release.yml
effort: medium
created: 2026-03-28
updated: 2026-10-02
author: changkun
dispatched_task_id: null
---

# Hosted Board Deployment

What it takes to run the task board (`wallfacer run`) as a hosted workload,
beside the public site and coordinator that are deployed today.

This spec has been rescoped twice. Its first version had wallfacer provision
its own sandbox execution in the cluster (Jobs, a node pool, network policy).
Its second version (2026-05-30) removed that and dispatched to a standalone
sandbox service through a `--executor cella` flag, with workspace files on a
file data plane. Neither service exists as a separate product any more. Under
the [platform integration](latere-integration.md), a hosted board has one
execution path, a hosted agent session, and no sandbox or file-plane
dependency of its own.

## What is deployed today

One workload, `wallfacerd`, which runs `wallfacer web` at `wf.latere.ai`:

| Role | Detail |
|---|---|
| Public site | The landing page and the documentation |
| Coordinator | The coordination plane's server role: the WebSocket endpoint local instances dial, the instance registry, and cloud-resident spec comments in Postgres |
| Sign-in | The public OAuth client against `auth.latere.ai`; the API verifies actor tokens addressed to its own audience and nothing else |

Its manifests are a kustomize base with production and staging overlays
(`deploy/base`, `deploy/prod`, `deploy/staging`), its image is built by
`Dockerfile.wallfacerd`, and `.github/workflows/release.yml` builds, applies
and smoke-tests it on a release. It keeps no task data and runs no agent.

## What a hosted board adds

A second workload from the same image, running `wallfacer run` in cloud mode
(`WALLFACER_CLOUD=true`: sign-in is forced, and records are scoped to the
principal and organization).

| Concern | Hosted board | Owner |
|---|---|---|
| Execution | Hosted only. Every task has the execution target `hosted`; the pod has no coding CLI installed and starts no agent process | [topos-remote-executor](latere-integration/topos-remote-executor.md) |
| Task and spec data | The filesystem store on a volume | this spec |
| Identity | The same issuer and client the site uses, with a second redirect address | this spec |
| Repositories | No user checkout exists on the server. See the blocking question below | open |
| Sandboxes, model keys, budgets | None in the pod | the platform |

### Workload

A Deployment modeled on `wallfacerd`, with these differences:

- Command `wallfacer run`, serving the board API and UI on `:8080`.
- A mounted data volume for the store.
- Resource requests sized for the store and the automation loops, not for
  the static site's footprint.
- Environment: `WALLFACER_CLOUD=true`, the `AUTH_*` set the site already
  carries with the board's own redirect address and audience, the hosted
  executor's variables (`WALLFACER_AGENTS_URL`, `WALLFACER_HOSTED_MODEL`,
  `WALLFACER_HOSTED_MAX_COST`), and no provider key. A hosted run's model
  access is the session's.
- A Service and an Ingress for the board's host, with the same TLS issuer
  and ingress class the site uses.

### Store

`FilesystemBackend` on a `ReadWriteOnce` volume. One replica follows from
that access mode, and the rollout replaces the pod instead of surging, as the
site's does. A store that allows more than one replica (Postgres and object
storage behind `StorageBackend`) stays deferred with the archived storage
tasks until one replica is the limit that is hit.

### Release

The existing release workflow gains the second workload: the same image, a
second `set image`, and a smoke check against the board's host. No second
image and no second pipeline.

## Blocking question: repositories without a user machine

The local product assumes the repository is on disk. Worktree setup, the
diff view, the commit pipeline's rebase and merge, the file explorer and the
spec tree all read a local checkout. A hosted board has none.

The hosted executor removes the need for a checkout during the run: the
session clones and pushes. It does not remove it for everything around the
run. Two shapes are possible, and choosing one is the first step before any
manifest is written:

| Shape | How | Cost |
|---|---|---|
| **A. Server-side clones** | The board keeps a clone of each workspace repository on its volume, fetched from the platform's git host with a token minted for the signed-in user. The existing worktree, diff and merge code runs unchanged against it | Source code at rest on the board's volume, per user. Volume size grows with repositories. A second audience to mint for |
| **B. No clone** | The board reads refs, diffs and file contents through the Repos API and merges there. Specs are read and written as repository files through the same API | A second implementation of every git-reading surface. Parity with local is work, not reuse |

A third shape, a board that manages tasks only for local instances and never
touches a repository, is the coordination plane's remote control and needs no
hosted board at all. See
[remote-control](../identity/remote-control.md).

## Not in this spec

- Sandbox scheduling, node pools, network policy for agent workloads. The
  platform's.
- A wallfacer-held model key or provider credential for hosted runs.
- A per-tenant instance provisioner or router. Archived with
  [multi-tenant](../.archive/cloud/multi-tenant.md); one board serves every
  signed-in principal, isolated by the principal and organization scoping
  that already ships.
- An external API for the board. Archived with
  [tenant-api](../.archive/cloud/tenant-api.md).

## Implementation tasks

| # | Task | Depends on |
|---|------|-----------|
| 1 | Decide the repository shape (A or B) and record the decision here | the product decision in the umbrella's open question 1 |
| 2 | Board Deployment, Service, Ingress and volume claim in `deploy/base`, patched by the overlays | 1, the hosted executor |
| 3 | Release workflow: roll the second workload and smoke-test the board's host | 2 |
| 4 | End to end on staging: sign in, create a task, run it hosted, restart the pod, confirm the task and its timeline persist and the run re-attaches | 2, 3 |
| 5 | Operator notes for the manifests and the secrets they name | 4 |

## Open questions

1. **Whether to ship it.** The umbrella's open question 1: a hosted board's
   address and product name are not decided. This spec is the deployment
   shape for that decision, not a commitment to it.
2. **Repository shape.** A or B above.
3. **Organization contexts.** A hosted board is most useful to a team, and
   the platform does not run an organization's agents yet. Until it does, a
   hosted board runs tasks in personal contexts only.
4. **Coordinator placement.** The coordinator lives in the site's workload
   today. Whether it stays there or moves into the board's workload once one
   exists affects which workload needs Postgres and the shared cache.
