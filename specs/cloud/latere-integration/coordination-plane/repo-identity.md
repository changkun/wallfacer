---
title: Workspace and Repo Identity
status: stale
depends_on:
  - specs/cloud/latere-integration/coordination-plane.md
affects:
  - internal/coordinator/
  - internal/handler/
  - internal/workspace/
  - frontend/src/
effort: large
created: 2026-06-15
updated: 2026-06-26
author: changkun
dispatched_task_id: null
---

# Workspace and Repo Identity

The data model under the whole coordination plane: how the coordinator knows and
trusts the repositories an org collaborates on, and what it stores versus what
stays on the client. Everything collaborative (presence, comments, the projection)
keys on this, so it is specified before the core is built. It answers the
question the earlier specs glossed: a wallfacer workspace is a `Group` of folder
**paths** (`internal/workspace/groups.go`), possibly several repos, so "the
workspace" is not one git remote.

## The collaboration unit is the Repo

Decided: presence and comments scope to a **repo**, identified by its canonical
GitHub coordinates `host/owner/repo` (`NormalizeRemoteURL`,
`internal/coordinator/identity.go`), not to a local folder and not to a
workspace-group. Rationale: "teammates working on the same project" means clones
of the same GitHub repo; the repo identity is the only thing stable across
machines, clones, and forks-of-record. A comment is keyed `(repo, spec_path,
anchor)`; presence is "who is on this repo". A workspace-group with three repos
simply registers three repo identities; collaboration happens per repo within it.

## Entities

| Entity | Source | Scope |
|--------|--------|-------|
| **Org** | Identity JWT `OrgID` | the tenant boundary (the security perimeter, below) |
| **Repo** | canonical `host/owner/repo` | the durable, org-scoped collaboration unit |
| **Workspace-group / folder** | `workspace.Group.Workspaces` (local paths) | per-machine, ephemeral; *resolves to* repos |
| **Instance** | a connected `wallfacer` | serves some repos right now |

## What is stored where

The folder-to-repo resolution stays on the **client**; the server stores repo
**identities**, never folders. This keeps local paths off the server (the data
boundary forbids repo paths) and makes the repo the stable anchor.

| Fact | Lives | Durability |
|------|-------|------------|
| Local folder paths (`Group.Workspaces`) | client only | never sent (local, sensitive, meaningless cross-machine) |
| Repo identity (`host/owner/repo`, derived client-side from each folder's `origin`) | sent in the manifest | the only workspace fact that crosses |
| Instance -> [verified repos it serves now] | Valkey (`wf:coord:ws:<remote>`) | ephemeral, rebuilt from manifests |
| Org -> [Repo records]; per-repo comments + projection rollups | Postgres (`wallfacer` db) | durable, outlives any connection |

So "how is the folder/repo/GitHub association stored on the server": **not as a
folder mapping at all.** The client resolves folder -> `git remote origin` ->
`host/owner/repo` and sends only the identity. The server keeps a durable, org-
scoped **Repo record** (the stable anchor for comments and history) plus the
ephemeral set of instances currently serving it. A repo record persists even when
no instance is connected, so a comment thread survives every teammate going
offline.

## The org boundary is the security perimeter

The coordinator keys every query and channel on `OrgID` from the validated JWT, so
**cross-tenant access is structurally impossible**: an instance in org A can never
see org B's repos, presence, or comments, regardless of what repo URL it claims.
Repo verification (next) is therefore a *within-org granularity* control, does
seeing repo R's threads require access to R, or just membership in the org that
owns R, not a cross-company firewall. The worst case a verification gap allows is
an insider seeing their own org's repo metadata.

## Repo verification (layered)

A manifest's repo claim is self-asserted, so within an org the coordinator
establishes "principal P may collaborate on repo R" by one of three mechanisms,
which all resolve to the same downstream fact and differ only in how it is
proven. Default is the lightest that still proves access; orgs opt into stronger
or simpler tiers.

### Default: local credential proof

The local instance proves access using the user's **own local git credentials**
(the same ones it already clones and pushes with): it runs a read-only
`git ls-remote <origin>` (or `gh repo view`) and reports success per repo in the
manifest. The coordinator binds that attestation to the JWT principal and marks
the `(principal, repo)` pair verified, with a re-check TTL.

- No external GitHub App, no extra OAuth, works with github.com **and** GHES /
  self-hosted git. This is the enterprise-friendly path and the reason it is the
  default: the platform's centrally-brokered GitHub App was retired (auth dropped
  `github_app_installations`; the sandbox creds-proxy that minted installation
  tokens is archived), so wallfacer does not reintroduce central brokering.
- It is genuinely *verified*, not *asserted*: you cannot `ls-remote` a repo you
  cannot reach. It is **client-attested** (the coordinator trusts the JWT-bound
  instance's report of its local check), which is acceptable because the org
  boundary already contains the blast radius to within-org.

### Fallback: org-admin-registered repos + membership

For locked-down orgs that will not do any GitHub integration (air-gapped, policy):
an org admin registers the repos the org collaborates on (a durable list on the
org's Repo records), and **org membership** grants collaboration on any registered
repo. No per-user GitHub check. Coarser (org-wide, not per-repo), but zero GitHub
dependency.

### Upgrade: per-user GitHub OAuth (server-authoritative)

For orgs wanting the coordinator to verify access **server-side** rather than
trust the client: reuse Identity's existing GitHub federated login
(`GITHUB_CLIENT_ID/SECRET`) extended with a repo-read scope, and have the
coordinator check `(github_user, owner/repo)` via the GitHub API. Strongest
within-org granularity; github.com-centric and needs per-user consent plus token
refresh. Optional, not the default.

### How the tiers compose

A repo is collaborable for a principal if **any** active tier grants it: the
instance proved local access, OR the repo is org-registered and the principal is
a member, OR server-side OAuth confirmed access. The coordinator records the
grant and its provenance (which tier) for audit. Tier selection is org config
(default: local-proof on, registry available, OAuth off).

## Manifest and verification flow

The manifest (`internal/coordinator`) carries, per served workspace, the repo
identity plus a verification claim:

```json
"workspaces": [
  {"remote": "github.com/latere-ai/wallfacer",
   "verified": "local-proof",        // or "org-registry" | "oauth" | "unverified"
   "proof": "ls-remote-ok"}          // tier-specific evidence, never a path or token
]
```

The coordinator does not trust `verified` blindly: for `org-registry` it checks
the org's registered list and the principal's membership; for `oauth` it performs
the server-side check; for `local-proof` it accepts the JWT-bound attestation
(the org boundary is the backstop). An `unverified` repo joins nothing. The proof
field never carries a local path, a token, or repo contents (data boundary).

## Non-goals

- Storing local folder paths server-side. Never; only repo identities cross.
- Reintroducing a centrally-brokered GitHub App. The platform retired it.
- Cross-org collaboration. The org boundary is absolute; an external collaborator
  is an Identity/org-membership concern, not this spec.
- Mirroring repo *contents* to the coordinator. Comments anchor to specs by path
  and content hash (see [spec-comments](spec-comments.md)); the coordinator never
  holds source.

## Open questions

1. **Local-proof recheck cadence.** How often the instance re-attests (access can
   be revoked on GitHub). A TTL plus re-attest on reconnect; tune the window.
2. **Org-registry admin surface.** Where admins register repos (a wf.latere.ai
   settings view) and whether registration auto-seeds from instances' verified
   repos.
3. **GHES under the OAuth upgrade.** Per-user OAuth is github.com-shaped; GHES
   would need its own OAuth app config. Local-proof already covers GHES, so OAuth
   may stay github.com-only.
4. **Repo identity for forks / renames.** A renamed or transferred GitHub repo
   changes `owner/repo`. Follow GitHub's rename redirect, or treat as a new repo
   and migrate threads? Likely a later concern, noted.
