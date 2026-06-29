---
title: Workspace Model
status: drafted
depends_on: []
affects:
  - internal/workspace/groups.go
  - internal/workspace/manager.go
  - internal/prompts/instructions.go
  - internal/store/agent_session_usage.go
  - internal/handler/config.go
  - internal/handler/workspace.go
  - internal/handler/handler.go
  - internal/runner/runner.go
  - internal/coordinator/wire.go
  - internal/cli/server.go
  - frontend/src/components/WorkspacePicker.vue
  - frontend/src/components/settings/SettingsTabWorkspace.vue
  - frontend/src/stores/tasks.ts
effort: xlarge
created: 2026-06-29
updated: 2026-06-29
author: changkun
dispatched_task_id: null
---

# Workspace Model

## Why this document

The workspace mechanism was designed under an assumption that no longer holds:
that an agent harness can only reach files inside one specified folder, so the
unit of work had to be a folder (or a small fixed set of folders) and its
identity could safely *be* that folder set. Harnesses now run on the host with
broad filesystem reach, so the folder boundary is no longer the security
boundary, and binding a workspace's identity to its folder set has become a
liability rather than a simplification.

The concrete defect: a workspace's identity is the hash of its sorted folder
paths. `Snapshot.Key = prompts.InstructionsKey(sortedPaths)`
(`internal/workspace/manager.go:236`) derives a 16-char key from
`sha256(paths joined by ":")[:8]` (`internal/prompts/instructions.go:20`), and
every scoped artifact lives under `~/.wallfacer/data/<key>/`: the task store,
agent-session transcripts, planning state, the whiteboard scene. Change the
folder set and the key changes, so all of that history is silently orphaned. The
`~/.wallfacer/migration-backup-44aff111a6bc69f4-to-ac1b309d91df901f/` artifact on
disk is the scar from the last time this bit. A live inventory finds 26 data
directories backing only 9 current groups; 17 are stranded history (two of them
substantial, with 102 and 64 tasks), reachable today only by re-selecting the
exact original paths.

This document redefines the workspace as a first-class object with a stable
identity decoupled from its membership, owned by a personal user or an
organization, with a folder set that the owner can change freely without losing
history. It is the foundation the cloud workspace objects and multi-repo
association will later build on; those two are explicitly out of scope here (see
[Non-goals](#non-goals)).

## Status quo (what exists)

The current vocabulary is inverted relative to the target. Today a single folder
path is called a "workspace" and a *set* of folders is a `Group`
(`internal/workspace/groups.go:16`). A `Group` already carries the ownership
fields this redesign needs (`CreatedBy`, `OrgID`), per-group automation toggles
(`Autopilot`, `Autotest`, `Autosubmit`, `Autosync`), and concurrency overrides
(`MaxParallel`, `MaxTestParallel`). Groups persist as a JSON array in
`~/.wallfacer/workspace-groups.json` and are visibility-filtered by principal via
`GroupsForPrincipal` (`groups.go:217`), which already implements the
local / personal / org isolation matrix.

What is missing is a stable identifier. A group is keyed by `GroupKey`
(newline-joined sorted paths, `groups.go:281`) for in-memory dedup and by
`InstructionsKey` (the hashed variant) for on-disk data. Both are pure functions
of the path set, so the path set *is* the identity.

Key derivation is consumed in five places that this redesign must redirect:

| Site | Today |
|------|-------|
| `manager.go:236` | `key := InstructionsKey(validated)` then `ScopedDataDir = data/<key>` |
| `manager.go:141` | `NewStatic` derives the key from paths |
| `config.go:231` | re-derives the key from a group's paths for the API response |
| `store/agent_session_usage.go:17` | re-derives the key from paths for usage attribution |
| `coordinator/wire.go:69` | `WorkspaceRef.LocalKey = GroupKey(paths)` for cross-instance join |

The name `InstructionsKey` is historical: the key once addressed a
wallfacer-managed per-workspace instructions file. No such file is written today.
The `CLAUDE.md` / `AGENTS.md` constants in `internal/prompts/instructions.go` name
the *per-repo* files an agent reads inside a folder; those are owned by the repo,
not by wallfacer, and are unaffected by this work. The user requirement "no
workspace-level CLAUDE.md" is therefore already the reality; this redesign only
makes it explicit and renames the key to reflect its actual role.

## Goals

1. A workspace is a first-class object with a stable, unique identity (a UUID)
   that never changes when its folder membership changes.
2. A workspace owns a *mutable* set of folder paths. The owner can add or remove
   folders at any time; task history, transcripts, planning state, and the
   whiteboard scene stay attached to the workspace across such edits.
3. A workspace is owned by either a personal user or an organization. Personal
   owners create their own workspaces; organization workspaces are created by an
   org admin. Visibility follows the existing principal isolation matrix.
4. A folder may belong to any number of workspaces simultaneously. There is no
   uniqueness constraint on membership.
5. Migration preserves all live history with zero data movement, and adopts
   substantial orphaned history rather than stranding it.
6. The vocabulary lands consistently across the Go packages, the on-disk format,
   the HTTP API, and the Vue frontend: the owned set of folders is a
   "Workspace"; a single member directory is a "folder".

## Non-goals (deferred, with seams noted)

- **Cloud workspace objects and sync.** Logged-in latere.ai users will see their
  workspaces as cloud-defined objects and reconcile cloud vs local membership.
  This document only establishes the local stable-identity model and plumbs the
  owner fields so the cloud layer has something to project onto. The cloud sync
  protocol, conflict resolution, and the cloud-side object schema are out of
  scope and tracked under the Cloud Platform track.
- **Multi-repo / GitHub association.** A workspace will later associate with one
  or more git(hub) repositories. Not in this document. The workspace record
  reserves no repo field yet; it is added when that work lands.
- **Filesystem sandboxing of workspace folders.** Constraining execution and
  file access to a workspace's folder set (the original assumption, re-imagined
  as an opt-in safety boundary) is explicitly future work. Membership remains
  pure metadata that does not gate any function.
- **Org admin RBAC for workspace creation.** Enforcing "only an org admin may
  create an org workspace" depends on the role matrix in
  `identity/multi-user-collaboration`. This document plumbs the owner field and
  leaves the create-permission gate to that track; locally (nil principal) any
  workspace may be created.

## The model

### Workspace record

```go
// Workspace is an owned, stably-identified set of folder paths. Its identity
// (ID) and its storage handle (DataKey) are independent of its membership
// (Folders), so the owner may change folders without orphaning history.
type Workspace struct {
    ID      string   `json:"id"`              // stable UUIDv4, assigned once at creation
    Name    string   `json:"name,omitempty"`  // optional human label
    Folders []string `json:"folders"`         // mutable; absolute, clean, sorted, deduped
    DataKey string   `json:"data_key"`        // stable storage handle under data/<DataKey>

    // Ownership (carried forward from Group; same semantics as store.Task).
    CreatedBy string `json:"created_by,omitempty"` // principal sub; empty = legacy/local
    OrgID     string `json:"org_id,omitempty"`     // org scope; empty = personal/legacy

    // Per-workspace automation + concurrency (carried forward from Group).
    MaxParallel     *int  `json:"max_parallel,omitempty"`
    MaxTestParallel *int  `json:"max_test_parallel,omitempty"`
    Autopilot       *bool `json:"autopilot,omitempty"`
    Autotest        *bool `json:"autotest,omitempty"`
    Autosubmit      *bool `json:"autosubmit,omitempty"`
    Autosync        *bool `json:"autosync,omitempty"`

    // Lifecycle bookkeeping.
    Dormant   bool   `json:"dormant,omitempty"`   // recovered history with no/uncertain folders
    CreatedAt string `json:"created_at,omitempty"`
    UpdatedAt string `json:"updated_at,omitempty"`
}
```

### Identity vs storage: the `DataKey`

The pivotal decision. `ID` is the *identity* the UI and ownership reason about.
`DataKey` is the *storage handle* that addresses `data/<DataKey>/` and keys the
in-memory `activeGroups` map, the runner's task-to-store routing, and the
agent-session fingerprint. The two are separate so that:

- **Migration moves zero bytes.** A migrated workspace sets
  `DataKey = InstructionsKey(its original paths)`, i.e. the existing 16-char hash
  that already names its data directory on disk. Nothing is renamed or copied.
- **Identity is first, paths are incidental.** A *new* workspace gets a fresh
  random `DataKey` (independent of its folders). Two distinct workspaces pointing
  at the same folders therefore have distinct storage and distinct history; a new
  workspace that happens to share folders with a migrated one correctly starts
  empty rather than inheriting the migrated workspace's tasks.
- **Membership edits never touch storage.** Adding or removing a folder mutates
  `Folders` only. `DataKey` is immutable for the life of the workspace.

`Snapshot.Key` (`manager.go:25`) becomes `workspace.DataKey`, obtained by lookup
rather than by recomputation from paths. Every downstream consumer
(`activeGroups`, `ScopedDataDir`, `runner.taskWSKey` / `taskStore`,
`DecrementAndCleanup`, the agent-session fingerprint) keeps working unchanged
because it already operates on an opaque `Key` string; only the *source* of that
string changes from "hash of current paths" to "the workspace's DataKey".

The five derivation sites enumerated in [Status quo](#status-quo-what-exists)
must each stop recomputing the key from paths and instead read the workspace's
`DataKey`. `coordinator/wire.go`'s `LocalKey` is a cross-instance join handle and
should likewise switch to `DataKey` (it is stable across path edits, which is the
correct behavior for cross-machine identity); the cloud-facing `Remote` git URL
is untouched.

`InstructionsKey` is renamed to `WorkspaceDataKey` and retained only as the
*seed* function used during migration. New-workspace key generation is a separate
random generator. This makes the "random for new, hash-seeded only for migration"
rule explicit at the type level rather than implicit in call order.

## On-disk format and migration

### New format

`~/.wallfacer/workspace-groups.json` is superseded by
`~/.wallfacer/workspaces.json`, a JSON array of `Workspace` records. MRU ordering
(used for session restore) is preserved by array position, exactly as today.

### Migration (one-time, automatic, backed up)

Runs once at startup when `workspaces.json` is absent and either
`workspace-groups.json` or orphaned `data/<hash>/` directories exist. The whole
operation is wrapped in a backup (a tarball of `workspace-groups.json` plus a
manifest of the data directories touched, under
`~/.wallfacer/migration-backup-workspaces-<timestamp>/`) and is idempotent: a
second run with `workspaces.json` present is a no-op.

1. **Live groups → workspaces.** For each group in `workspace-groups.json`, mint
   a workspace with a fresh `ID`, copy `Name` / `Folders` (renamed from
   `workspaces`) / ownership / toggles, and set
   `DataKey = WorkspaceDataKey(group.Folders)`. The data directory already exists
   under that key; nothing moves. `Dormant = false`.

2. **Orphaned data dirs → dormant recovered workspaces.** Enumerate
   `data/<hash>/` directories whose hash matches no live group. For each that is
   **non-empty**, mint a workspace with a fresh `ID`,
   `DataKey = <hash>` (the directory name), and `Dormant = true`. Best-effort
   recover its `Folders` by scanning the contained `task.json` records for source
   paths (the `worktree_paths` and `environment` fields embed them); if recovery
   yields nothing, leave `Folders` empty and let the owner re-point later. Name
   it `Recovered <short-hash>` when no better label is available. **Empty**
   orphan dirs are dropped from surfacing (left on disk, not deleted, not listed).

3. **Ownership inheritance.** Dormant recovered workspaces inherit no owner
   (`CreatedBy` / `OrgID` empty = legacy), so they appear only in local and
   personal-legacy views, never in an org view, matching the existing isolation
   rules.

The decision to adopt non-empty orphans as dormant (rather than delete or
silently leave them) is a product choice confirmed during design: it guarantees
no visible history loss while keeping the workspace list free of empty shells.

## Runtime: the manager

`workspace.Group` is renamed `workspace.Workspace` and gains the fields above.
`workspace.Manager` switches *by workspace ID* rather than by path set:

- `Switch(id string)` replaces `Switch(paths []string)`. It loads the workspace
  record, validates its folders (a folder that has since been deleted or
  unmounted is skipped with a warning, not fatal, mirroring the current
  `startupWorkspaces` tolerance), and installs a snapshot whose `Key` is the
  workspace's `DataKey`.
- A new `UpdateFolders(id string, folders []string)` mutates membership in place:
  it re-validates, persists the workspace record, refreshes the active snapshot's
  `Workspaces` slice, and republishes, but **does not** change `DataKey`, open a
  new store, or move data. This is the operation that was impossible before.
- `Create`, `Rename`, and `Delete` operate on workspace records. `Delete` removes
  the record and may optionally archive (not destroy) its data directory.
- `NewStatic` and the CLI server wiring (`cli/server.go`) construct or look up a
  workspace by ID instead of synthesizing one from paths.

`GroupsForPrincipal` becomes `WorkspacesForPrincipal` with identical logic.
`ClaimGroup` is retired: ownership is stamped at `Create` time now that there is
an explicit creation step, rather than lazily on first switch.

## HTTP API

The path-browsing helpers (`/api/workspaces/browse`, `/mkdir`, `/rename`) are
unchanged. The membership surface gains explicit CRUD:

| Method + path | Purpose |
|---------------|---------|
| `GET /api/workspaces` | list workspaces visible to the principal (id, name, folders, dormant, toggles) |
| `POST /api/workspaces` | create a workspace (name + initial folders); stamps owner from principal |
| `PUT /api/workspaces/{id}` | rename and/or replace the folder set (the membership edit) |
| `DELETE /api/workspaces/{id}` | delete the workspace record |
| `POST /api/workspaces/{id}/activate` | switch the active workspace by ID |
| `GET /api/config` | now returns the active workspace's `id` alongside its folders |

The legacy `POST /api/workspaces {workspaces:[...]}` switch-by-paths shape is
removed; callers move to `activate` by id. `config.go`'s response stops
recomputing the key from paths and reads `DataKey` from the active record.

## Frontend

- `WorkspacePicker.vue` becomes a workspace chooser: list existing workspaces
  (by name/folders) to activate, plus a "new workspace" path that creates a
  record. Folder browsing is reused as the folder-add step.
- `SettingsTabWorkspace.vue` becomes the workspace editor: rename, add/remove
  folders (calling `PUT /api/workspaces/{id}`), edit toggles, delete. Dormant
  recovered workspaces render with a "re-point folders" affordance.
- `DockWorkspace.vue` shows the active workspace name (falling back to folder
  basenames when unnamed).
- `stores/tasks.ts` tracks the active workspace `id` in addition to its folders.

Membership editing must feel non-destructive: after removing and re-adding a
folder, the board still shows the same tasks. This is the user-visible proof that
identity decoupled from membership.

## Acceptance criteria

1. A workspace has a UUID assigned at creation that is stable across folder edits
   and server restarts.
2. Removing a folder from a workspace and re-adding it (in either order, with a
   restart in between) leaves the task store, transcripts, planning state, and
   whiteboard scene intact and attached to the same workspace.
3. Two workspaces may list the same folder with no error and with fully separate
   history.
4. A freshly created workspace whose folders coincide with a migrated
   workspace's folders starts with an empty store (no inherited tasks).
5. Migration of the live `~/.wallfacer/workspace-groups.json` produces a
   `workspaces.json` where every one of the 9 live groups maps to a workspace
   whose `DataKey` equals its prior `InstructionsKey`, and no `data/<hash>/`
   directory is moved or copied.
6. Each non-empty orphaned data directory becomes a dormant workspace visible in
   the (local / personal-legacy) workspace list; empty ones are not surfaced and
   not deleted.
7. Migration is idempotent: re-running with `workspaces.json` present changes
   nothing.
8. Principal visibility for workspaces matches the prior group matrix (local sees
   all; personal sees own + legacy; org sees only same-org, strictly).
9. `make build` and `make lint` pass.

## Test plan

Every behavior above is covered by a failing-first test before its fix, per the
project rule.

- `internal/workspace`: unit tests for `Create` / `UpdateFolders` /
  `Switch(id)` / `Delete` proving identity stability across folder edits, the
  empty-start guarantee for new workspaces sharing folders, and same-folder
  multi-workspace independence. Reuse the `newStore` factory injection already in
  `manager_test.go` to assert no store re-creation on `UpdateFolders`.
- A migration test fixture: a synthetic `workspace-groups.json` plus a set of
  `data/<hash>/` dirs (some matching groups, some non-empty orphans, some empty),
  asserting the mapping, the dormant adoption, the zero-move property (inode /
  mtime unchanged), and idempotency on second run.
- `WorkspacesForPrincipal` table test mirroring the existing
  `groups_principal_test.go` matrix.
- Handler tests for the CRUD routes including principal-scoped visibility and the
  `activate`-by-id switch.
- A frontend test asserting that an `UpdateFolders` round-trip preserves the
  active workspace id and task list.

## Risks and open questions

- **Key-source audit completeness.** The redesign is only correct if *every* site
  that addresses `data/<key>/` reads `DataKey` rather than recomputing from
  paths. The five known sites are enumerated above; the implementation must grep
  for all callers of `InstructionsKey` / `GroupKey` / `filepath.Join(dataDir, …)`
  and confirm none recompute-and-expect-match. A missed site silently orphans
  data on the first folder edit, reproducing the original bug.
- **Orphan path recovery quality.** Best-effort recovery from `task.json` may
  yield partial or stale paths (a repo since moved). Recovered `Folders` are a
  convenience hint, not a guarantee; dormant workspaces must remain usable with
  empty folders.
- **Coordinator `LocalKey` change.** Switching `WorkspaceRef.LocalKey` from
  `GroupKey` to `DataKey` changes the cross-instance join handle. Confirm no
  persisted coordinator state keys off the old value, or migrate it alongside.
- **`workspace-groups.json` readers outside the workspace package.** Confirm no
  other component reads the old file directly; all access should route through
  the workspace package so the rename is contained.

## Proposed breakdown (non-leaf)

This is a design spec, to be decomposed via `wf-spec-breakdown` into roughly:

1. **Record + key seam** (Go): introduce `Workspace`, `DataKey`, rename
   `InstructionsKey` → `WorkspaceDataKey` + add the random generator, redirect
   the five derivation sites. No behavior change yet (key still seeded from
   paths). Lands the seam under test.
2. **Manager by-ID + membership edit**: `Switch(id)`, `UpdateFolders`,
   `Create/Rename/Delete`, `WorkspacesForPrincipal`, retire `ClaimGroup`.
3. **Migration**: `workspaces.json`, live-group mapping, dormant orphan adoption,
   best-effort path recovery, backup + idempotency.
4. **HTTP API**: CRUD routes + `activate`, config response carries the id.
5. **Frontend**: chooser, editor, dock label, store id tracking.

Step 1 is the keystone and should land and bake before the rest, since it is the
change that, if incomplete, reintroduces the orphaning bug.
