# Specs

Wallfacer roadmap, organized by track and connected by shared design foundations.
Completed specs live in the [Archive](#archive) section at the bottom.

## Status Quo

What has shipped vs what remains. ✅ = complete, ◐ = in progress, ○ = not started, ⊘ = archived.

```
Foundations — 7/7 complete (see Archive)

Local Product — 9 shipped, 1 in progress, rest pending
  ⊘ Desktop App (code removed)     ✅ Terminal Sessions
  ✅ Container Exec                ✅ OAuth Token Setup
  ✅ Pixel Agent Avatars           ◐ Spec Coordination
  ✅ Routine Tasks                 ✅ Agents & Flows
  ✅ Refinement Into Plan          ✅ Vue Frontend Migration
  ✅ Rebrand Module Path           ○ Scoped Command Registry
  ○ Host Mounts                    ○ Live Serve
  ○ Terminal UI (TUI mode)         ○ Excalidraw Whiteboard
  ○ Task Prompt Attachments        ○ Inline File Panel
  ○ Diff Review Comments
  ⊘ superseded by the Vue/host rewrite: File Attachments,
    File Panel Viewer, Inline Diff Feedback, Spatial Canvas

Cloud Platform — integration track (consume Latere services, don't absorb)
  ○ Latere Integration (umbrella)  ○ Runtime → Cella
  ○ Tenant Filesystem → FS         ○ Cloud Infrastructure
  ○ Tenant API
  archived: Multi-Tenant, Billing Idempotency (now owned by Cella / Identity)

Shared Design — 3 complete
  ✅ Agent Abstraction             ✅ Host Exec Mode
  ✅ Host as Only Backend          ◐ Harness Abstraction (interface + Claude/Codex shipped)
  ○ Token & Cost Optimization      ○ Extensible Prompts
  ⊘ Overlay Snapshots (premise obsolete under host exec)

Intent — 0/3 (every action is a commit; undo and PR build on it)
  ○ Intent-Driven Commits          ○ Task Revert
  ○ Pull Request Creation

Identity — 1/6 (principals, sessions, data boundaries)
  ✅ Authentication                ○ Third-Party OIDC
  ○ Remote Control                 ○ Agent Token Exchange
  ○ Multi-User Collaboration       ○ Data Boundary Enforcement

Oversight — 0/2 (task-level verification gates)
  ○ Test Criteria (replaces Validation Barrier)   ○ Visual Verification
```

---

## Local Product

Desktop experience and developer workflow improvements. No cloud dependency. Ships value to single-user deployments.

| Spec | Status | Delivers |
|------|--------|----------|
| [spec-coordination.md](local/spec-coordination.md) | In progress | Umbrella: recursive spec tree model, dispatch workflow, cross-task context |
| ↳ [spec-document-model.md](local/spec-coordination/spec-document-model.md) | **Complete** | Spec frontmatter schema, filesystem-derived tree, `depends_on` DAG, six-state lifecycle (including `archived`), per-spec and cross-spec validation, recursive progress tracking, impact analysis. Extracted `internal/pkg/dag/`, `internal/pkg/tree/`, `internal/pkg/statemachine/` |
| ↳ [spec-archival.md](local/spec-coordination/spec-archival.md) | **Complete** | Sixth lifecycle state (`archived`) — hidden by default, read-only, excluded from impact / progress / drift / stale-propagation. Cascades over non-leaf subtrees on archive; unarchive reverses via `git revert` of the archive commit. Muted rendering in explorer and minimap; archived banner in focused view with stacked undo toasts. |
| ↳ [spec-planning-ux.md](local/spec-coordination/spec-planning-ux.md) | **Complete** | Three-pane spec mode (explorer, focused markdown view, chat stream), planning sandbox container, chat-driven spec iteration, dispatch & board integration, undo snapshots, planning cost tracking. Deferred: Codex compatibility, enhanced session recovery. |
| ↳↳ [chat-first-mode.md](local/spec-coordination/spec-planning-ux/chat-first-mode.md) | **Complete** | Rename "Spec" to "Plan", collapse the layout when no specs exist, `/create` directive parser and bootstrap choreography. All 14 leaf tasks shipped; rolled up from `validated`. |
| ↳↳ [planning-chat-threads.md](local/spec-coordination/spec-planning-ux/planning-chat-threads.md) | Drafted | Multi-tab planning chat: independent conversation threads per workspace group sharing the single planner sandbox, per-thread session/history, inline rename, archive-only deletion, `git revert`-based thread-scoped undo, crash-safe migration from single-thread layout. |
| ↳ [spec-state-control-plane.md](local/spec-coordination/spec-state-control-plane.md) | Not started | Server-managed lifecycle transitions: chat-edit fan-out → `stale`, dispatch → `validated`, task done → tester-mediated drift verdict → `complete` or `stale`, periodic cross-tree staleness scan, downstream propagation via `depends_on`. |
| [excalidraw-whiteboard.md](local/excalidraw-whiteboard.md) | Not started | Excalidraw-based drawing/brainstorm whiteboard as a peer view |
| [task-prompt-attachments.md](local/task-prompt-attachments.md) | Drafted | Drag-and-drop file and image attachments for task prompts; worktree `.attachments/` staging + Read tool. Supersedes the archived file-attachments. |
| [host-mounts.md](local/host-mounts.md) | Not started | Per-task read-only host filesystem mounts into sandbox containers |
| [inline-file-panel.md](local/inline-file-panel.md) | Drafted | Inline tabbed file panel with multi-modal preview (image/video/audio/PDF/hex) over `ExplorerPanel.vue` + a raw-content endpoint. Supersedes the archived file-panel-viewer. |
| [diff-review-comments.md](local/diff-review-comments.md) | Drafted | Code-review-style inline comments anchored to diff lines, batched into the existing feedback channel. Supersedes the archived inline-diff-feedback. |
| [live-serve.md](local/live-serve.md) | Drafted | Build and run developed software from within Wallfacer |
| [refinement-into-plan.md](local/refinement-into-plan.md) | **Complete** | Retired the bespoke refine pipeline. Plan mode edits task prompts directly via a Task Prompts explorer section and a task-aware `update_task_prompt` tool. Rounds persist as task events; undo is event rewind for task mode, git revert for spec mode. Auto-refine removed entirely (no replacement in this spec). |
| [terminal-ui.md](local/terminal-ui.md) | Not started | Full TUI mode — interactive terminal board, log streaming, task lifecycle via Bubble Tea |
| [backend-redundancy-cleanup.md](local/backend-redundancy-cleanup.md) | **Complete** | Umbrella for follow-ups to the June 2026 pass-1 cleanup. All children landed or retired: backend-only leaves done in pass 1 (two declined), and the 6 API-surface leaves (planning threads, spec actions, oversight phase, task actions, auth orgs, ideate facade) collapsed verb-specific routes into PATCH/parameterised endpoints (123 → 113 routes). `ideate` archived (premise stale); `transitionTask` helper deferred. |
| [rebrand-module-path.md](local/rebrand-module-path.md) | **Complete** | Migrated the module path and import refs from `changkun.de/x/wallfacer` to `latere.ai/x/wallfacer` (267 files, `ffce4807`). Two follow-ups (wallfacerd image rename, vanityOwners override removal) are deferred, gated on the repo moving into the `latere-ai` GitHub org. |
| [scoped-command-registry.md](local/scoped-command-registry.md) | Drafted | Promote the planning-only slash command registry to a surface-agnostic mechanism with per-scope catalogs (planning, task_create, task_waiting). Task board and other UI surfaces can then trigger their own `/` commands via the shared autocomplete widget. |
| [routine-tasks.md](local/routine-tasks.md) | **Complete** | Promote the ideation agent's "cronjob" scheduler into a generic primitive: routines are board cards (`Kind=routine`) with a schedule that spawn fresh instance tasks when they fire. Users create, edit, and toggle routines on the board; ideation migrates to a `system:ideation`-tagged routine. |
| [agents-and-flows.md](local/agents-and-flows.md) | **Complete** | Promote agent role + pipeline to first-class user-facing primitives. Sidebar gains Agents and Flows tabs; the composer simplifies to "pick a Flow, write a prompt". Seeded built-in flows (`implement`, `brainstorm`, `refine-only`, `test-only`) replace the current TaskKind + Agent-overrides surface. Depends on the backend abstraction in `shared/agent-abstraction.md`. |
| [agents-and-flows/refinements.md](local/agents-and-flows/refinements.md) | **Archived** | Post-ship follow-ups to the agents-and-flows track that landed after the parent was archived: split-pane UI redesign for both tabs, token-based CSS restyle onto the paper-ink palette, `Role.PromptTmpl` runtime wiring in `runAgent`, dedicated [`docs/guide/agents-and-flows.md`](../docs/guide/agents-and-flows.md) guide, and a cross-reference repair across 12 docs. |

Archived local specs (superseded or dropped) are listed in the [Archive](#local--archived-superseded-or-dropped) section below.

### Local product dependencies

```mermaid
graph LR
  FE[File Explorer ✅] --> SC[Spec Coordination ◐]
  FE --> TPA[Task Prompt Attachments]
  FE --> HM[Host Mounts]
  FE --> IFP[Inline File Panel]
  HT[Host Terminal ✅] --> TS[Terminal Sessions ✅] --> CE[Container Exec ✅]

  EW[Excalidraw Whiteboard]
  DRC[Diff Review Comments]
  PR[Pull Request Creation]
  TR[Task Revert]
  LS[Live Serve]
  OTS[OAuth Token Setup ✅]
  TUI[Terminal UI]
  PA[Pixel Agents ✅]
  VFM[Vue Frontend Migration ✅]

  style FE fill:#d4edda,stroke:#28a745
  style HT fill:#d4edda,stroke:#28a745
  style TS fill:#d4edda,stroke:#28a745
  style CE fill:#d4edda,stroke:#28a745
  style OTS fill:#d4edda,stroke:#28a745
  style PA fill:#d4edda,stroke:#28a745
  style VFM fill:#d4edda,stroke:#28a745
  style SC fill:#fff3cd,stroke:#ffc107
```

---

## Cloud Platform

Integration track. Wallfacer is the autonomous-engineering control plane; in cloud mode it **consumes** Latere platform services rather than absorbing them (`latere.ai/specs/products/wallfacer.md`). Each integration is a thin client over a service boundary (Identity, Cella, FS), config-gated so local mode is unchanged. The earlier "build our own control plane / instance provisioner / Stripe" specs are archived — Cella and Identity own those concerns now.

| Spec | Status | Delivers |
|------|--------|----------|
| [latere-integration.md](cloud/latere-integration.md) | Drafted | Umbrella: the integration seams (Identity ✅, Runtime→Cella, FS, deploy, Lux, MCP, metadata) and the consume-don't-absorb rules |
| ↳ [latere-integration/cella-runtime.md](cloud/latere-integration/cella-runtime.md) | Drafted | `CellaBackend` implementing `executor.Backend` — a cloud runtime alongside Host, selected by `--backend cella`. Maps `ContainerSpec` onto Cella's `/v1/sandboxes` API; worktree transport via FS. (Spec still names `internal/sandbox`; reconcile to `internal/executor` when picked up.) |
| ↳ [latere-integration/shared-cella-client.md](cloud/latere-integration/shared-cella-client.md) | Drafted | Extract Cella's wire client into a standalone `latere.ai/x/sandbox/client` package shared by Wallfacer's `CellaBackend` and Topos's `cella.Provider`; abstractions stay split (harness-inside vs harness-outside) |
| ↳ [latere-integration/topos-remote-executor.md](cloud/latere-integration/topos-remote-executor.md) | Drafted | `TopozExecutor` — dispatch task runs to Latere Topos's `/v1/agents` control plane via `--executor topos`. Reuses Latere auth; v1 workspace transport via git push. Topos runs the harness on the remote side; client streams canonical events back. |
| [claude-managed-agents.md](cloud/claude-managed-agents.md) | Drafted | Third-party remote executor — dispatch tasks to Anthropic's Managed Agents API (`POST /v1/sessions`) with a self-hosted sandbox mounting the worktree locally. Self-contained executor: harness is fixed (Managed Agents), model selectable per agent version. Independent of Latere infra. |
| [antigravity.md](cloud/antigravity.md) | Drafted | Third-party remote executor — dispatch tasks to Google's Antigravity Interactions API. Self-contained executor: harness + model both fixed (Gemini 3.5 Flash). v1 requires a git-clonable workspace. Independent of Latere infra. |
| [tenant-filesystem.md](cloud/tenant-filesystem.md) | Drafted | fs.latere.ai integration, repo provisioner, workspace cloud mapping. **Blocked on FS Workspace API (Phase 5)** |
| [cloud-infrastructure.md](cloud/cloud-infrastructure.md) | Drafted | Thin deploy module into the existing DOKS `latere` namespace + `pkg/otel` OTLP emit |
| [tenant-api.md](cloud/tenant-api.md) | Drafted | Versioned external API (`/api/v1/`), per-tenant API keys, webhooks |

### Cloud platform dependencies

```mermaid
graph LR
  AUTH[Identity → Authentication ✅] --> LI[Latere Integration]
  LI --> CR[Runtime → Cella]
  LI --> TFS[Tenant FS]
  LI --> CI[Cloud Infrastructure]
  LI --> TA[Tenant API]
  SBX[Cella ext] --> CR
  FS[fs.latere.ai ext] --> TFS
  SBI[Sandbox Interface ✅] --> CR
  STI[Storage Interface ✅] --> TFS

  style AUTH fill:#d4edda,stroke:#28a745
  style SBI fill:#d4edda,stroke:#28a745
  style STI fill:#d4edda,stroke:#28a745
  style FS fill:#e8daef,stroke:#8e44ad
  style SBX fill:#e8daef,stroke:#8e44ad
```

### Deployment modes

Three modes, auth is opt-in at every mode (see [authentication.md](identity/authentication.md)):

1. **Local anonymous (today):** Wallfacer runs on the user's machine, no auth. Filesystem storage, host execution.
2. **Local authenticated:** Same binary, signed in to latere.ai. Adds account linkage and (later) cloud metadata coordination — code stays local, execution stays local.
3. **Cloud execution (Phase 3+, gated by demand):** Task runtimes dispatch to **Cella** via the runtime seam ([cella-runtime.md](cloud/latere-integration/cella-runtime.md)); workspace files stage through **FS**. Wallfacer asks "run this bounded task"; Cella owns scheduling, warm pools, and hardening.

Why no wallfacer-owned K8s control plane? Cella already owns sandbox runtime, lifecycle, quota, and audit. Wallfacer consuming Cella's `Runtime` interface avoids duplicating an entire platform — see [latere-integration.md](cloud/latere-integration.md).

---

## Shared Design

Specs that serve both tracks. These define interfaces and behaviors that local product and cloud platform both depend on.

| Spec | Status | Serves | Delivers |
|------|--------|--------|----------|
| [agent-abstraction.md](shared/agent-abstraction.md) | **Complete** | Both | `AgentRole` descriptor + `runAgent` primitive unify the seven sub-agent roles (title, oversight, commit, refinement, ideation, implementation, testing) onto one container launch path. Shipped Option A across 5 migration phases; Options C / D deferred. |
| [host-exec-mode.md](shared/host-exec-mode.md) | **Complete** | Local | `HostBackend` — opt-in `wallfacer run --backend host` that execs host-installed `claude`/`codex` directly. No image pull, no container; trades isolation for zero install friction. Covers both agents, live NDJSON streaming, parallel-cap default, Settings UI warning, `make build-host` target, and host-mode E2E harness. |
| [host-default.md](shared/host-default.md) | **Complete** | Local | Make host the only local backend — remove `LocalBackend`, the `--backend` flag, image pull plumbing, `codex-agent.sh`, the Sandbox Images UI surface, and `Dockerfile` (agent image). Cloud / Cella path unaffected. |
| [harness-abstraction.md](shared/harness-abstraction.md) | Validated | Both | New `internal/harness/` package with a `Harness` interface (BuildArgv / ParseEvent / AuthEnv / Capabilities). Renamed `sandbox.Type` → `harness.ID`. Interface + Claude/Codex migration **shipped**; Cursor, OpenCode, Pi remain drafted. Topos is a remote *executor*, not a harness (see cloud track). |
| ↳ [harness-abstraction/interface.md](shared/harness-abstraction/interface.md) | **Complete** | Both | Skeleton package: interface, value types, registry, fake harness for tests. No production caller migrates. |
| ↳ [harness-abstraction/claude-and-codex-migration.md](shared/harness-abstraction/claude-and-codex-migration.md) | **Complete** | Both | Move existing Claude / Codex argv + parse logic into `harness.Claude` and `harness.Codex`; delete `host_codex.go` and Codex's "fake a Claude result line" synthesis. |
| ↳ [harness-abstraction/cursor.md](shared/harness-abstraction/cursor.md) | Drafted | Both | Cursor (`cursor-agent`) harness — first validation case for the abstraction. |
| ↳ [harness-abstraction/opencode.md](shared/harness-abstraction/opencode.md) | Drafted | Both | OpenCode (`opencode run`) harness — provider auth managed by the harness itself. |
| ↳ [harness-abstraction/pi.md](shared/harness-abstraction/pi.md) | Drafted | Both | Pi (`pi` — earendil-works coding agent, not Inflection Pi) harness, JSON mode. |
| [token-cost-optimization.md](shared/token-cost-optimization.md) | Not started | Both | Cache observability, --resume correctness audit, shell output compression (RTK), consumption regression model, prospective budgeting. |
| [extensible-prompts.md](shared/extensible-prompts.md) | Not started | Both | Discoverable, user-creatable prompt system — replace hardcoded templates with skill-like prompt files that the system discovers at runtime. |
| [overlay-snapshots.md](shared/overlay-snapshots.md) | **Archived** | Both | Overlay snapshot cloning + CRIU checkpoint/restore for warm container startup. Archived: the per-task container model it optimized was removed in favor of host execution (tasks run directly in git worktrees, no container to snapshot). No replacement; a future container runtime would spec this fresh. |

### Why these are shared

**Agent abstraction** refactors `internal/runner/` — the execution engine that both tracks use. Without it, every new agent role requires touching 6+ files with duplicated launch/parse/usage logic. Both tracks add new roles (cloud adds K8s-aware agents, local product adds planning/gate agents from spec coordination).

**Harness abstraction** lets both tracks add coding agents (Cursor, OpenCode, Pi, and beyond) behind one interface instead of branching on agent type throughout the runner.

---

## Identity

Everything about principals, sessions, delegation, and what data crosses the machine boundary. Authentication is the anchor; the rest build on the principal context it establishes. Spans local, cloud, and cross-machine (remote-control) deployments — any wallfacer instance that has a user, an org, or an agent acting on behalf of either needs these.

| Spec | Status | Delivers |
|------|--------|----------|
| [authentication.md](identity/authentication.md) | **Complete** | OAuth2/OIDC login, session management, user identity. Phase 1: `WALLFACER_CLOUD` flag, `latere.ai/x/pkg/oidc` integration, cloud-gated `/login`/`/callback`/`/logout`/`/logout/notify`/`/api/auth/me` routes, status-bar sign-in badge. Phase 2: JWT middleware, principal context, `org_id`/`created_by` fields, forced login, superadmin/scope gating, org switching. Phase 3 split into third-party-oidc and remote-control below. Follow-up auth-unification (authkit.Identity adoption, HTTP device-code login) shipped and is archived under `identity/authentication/`. |
| [third-party-oidc.md](identity/third-party-oidc.md) | Vague | Pluggable OIDC so self-hosted non-latere.ai deployments can log in against Keycloak, Entra ID, Okta, Authelia, Dex, etc. Depends on authentication Phase 2. |
| [remote-control.md](identity/remote-control.md) | Vague | Wire protocol + latere.ai-side registry that lets the latere.ai web UI or a mobile client observe and operate signed-in local wallfacer instances. Depends on authentication Phase 2. |
| [agent-token-exchange.md](identity/agent-token-exchange.md) | Drafted | RFC 8693 delegation — mint short-lived agent tokens per task so sandbox agents can call latere.ai backend services (fs, telemetry) on behalf of the dispatching user. Orthogonal to user login. |
| [multi-user-collaboration.md](identity/multi-user-collaboration.md) | Drafted | Reframes tenant as org (not user), adds actor fields across the store, RBAC role matrix, presence/focus, optimistic concurrency, private planning threads. Blocker for cloud hosting. |
| [data-boundary-enforcement.md](identity/data-boundary-enforcement.md) | Drafted | Enforce what metadata can leave the user's machine to wallfacer.cloud — explicit allow-list, redaction at the boundary, CI lint against leaked code/paths/secrets. |

### Identity dependencies

```mermaid
graph LR
  AUTH[Authentication ✅] --> TPO[Third-Party OIDC]
  AUTH --> RC[Remote Control]
  AUTH --> ATE[Agent Token Exchange]
  AUTH --> MUC[Multi-User Collaboration]
  AUTH --> DBE[Data Boundary Enforcement]
  MUC --> CH[Cloud team hosting]
  DBE --> TO[Telemetry → Observability]

  style AUTH fill:#d4edda,stroke:#28a745
```

Multi-user collaboration is the gate for cloud *team* hosting (org-scoped shared boards); data-boundary-enforcement is the gate for anything the local instance sends up to observability / cloud.

---

## Intent

Every user or agent action becomes a git commit with metadata; revert and PR are natural consumers. Intent-commits is the primitive foundation — once every action is a commit, "undo this task" and "open a PR for this branch" are just traversals over that commit graph.

| Spec | Status | Delivers |
|------|--------|----------|
| [intent-commits.md](intent/intent-commits.md) | Vague | Every intent (task run, planning chat edit, explorer file edit) produces a git commit with structured metadata. Enables fine-grained undo, attribution, and revert. Foundation for the other two specs in this theme. |
| [task-revert.md](intent/task-revert.md) | Drafted | Agent-assisted revert of merged task changes with conflict resolution. Consumes intent-commits metadata to know which commits belong to a task. |
| [pull-request.md](intent/pull-request.md) | Drafted | Agent-generated GitHub PR from the current branch via a lightweight sandbox. Uses intent-commits metadata to pick commit messages and PR body content. |

### Intent dependencies

```mermaid
graph LR
  IC[Intent-Driven Commits] --> TR[Task Revert]
  IC --> PR[Pull Request Creation]
```

Ship intent-commits first; revert and PR become noticeably simpler once every action is already a well-formed commit.

---

## Oversight

Task-level verification gates wallfacer runs around its own agent execution.

| Spec | Status | Delivers |
|------|--------|----------|
| [test-criteria.md](oversight/test-criteria.md) | Drafted | Persist user-defined free-form test criteria on a task so the auto-tester checks them after the implementation phase (`Task.Criteria` into the existing `buildTestPrompt` / `tryAutoTest` path). Supersedes the archived validation-barrier (which was built on the removed Goal field and vanilla-JS UI). |
| [visual-verification.md](oversight/visual-verification.md) | Drafted | Post-execution visual check for UI changes — Playwright-based screenshot diffs. |

Both are independent, task-scoped, and depend only on the shipped agent abstraction.

---

## Ordering Rationale

**Within local product:**
- Spec coordination is in progress (document model, planning UX, archival, and chat-first mode complete; the state control plane / drift detection remains).
- Live serve is independent — start anytime.
- Oversight is two task-scoped gates (test criteria, visual verification) under `specs/oversight/`.

**Within cloud platform:**
- [latere-integration.md](cloud/latere-integration.md) is the umbrella — it defines each seam's contract and the consume-don't-absorb rules. Read it first.
- Runtime → Cella ([cella-runtime.md](cloud/latere-integration/cella-runtime.md)) is the lead leaf: it generalizes the existing Host backend selection to a cloud runtime via the `executor.Backend` interface.
- Tenant filesystem integrates with fs.latere.ai for config persistence and hot-tier workspace staging. Prerequisite: fs.latere.ai Phase 5 (Workspace API) — the cross-product critical-path blocker. The runtime leaf's worktree transport depends on it.
- Cloud infrastructure is a thin deploy module into the existing DOKS `latere` namespace, not a from-scratch cluster design.
- Tenant API comes last — after the runtime + FS seams exist.

**Cross-track:**
- Identity (authentication, OIDC, remote-control, agent-tokens, multi-user-collab, data-boundary) now lives in `specs/identity/` as its own theme — authentication complete; the rest unblocked by Phase 2.
- Agent abstraction reduces duplication before either track adds new agent roles.
- Sandbox backends (K8s, native-OS, hardening) live in the external `latere.ai/sandbox` repo and evolve on their own timeline; wallfacer depends on the `Runtime` interface that repo exposes.

**Between tracks:**
- The two tracks are independent after shared foundations. They can run in parallel.
- The only hard cross-track dependency: the cloud integration track requires authentication (shipped).

---

## Archive

System of record for completed work. Stable, not under active development. Included for reference and dependency context only.

### Foundations

Abstraction interfaces that all tracks build on. All seven are shipped and stable.

| Spec | Delivers |
|------|----------|
| [sandbox-backends.md](foundations/sandbox-backends.md) | `sandbox.Backend` / `sandbox.Handle` + `LocalBackend` |
| [storage-backends.md](foundations/storage-backends.md) | `StorageBackend` + `FilesystemBackend`; cloud backends (PG, S3) deferred to cloud track |
| [multi-workspace-groups.md](foundations/multi-workspace-groups.md) | Multi-store manager, runtime workspace switching |
| [container-reuse.md](foundations/container-reuse.md) | Per-task worker containers via `podman exec` |
| [file-explorer.md](foundations/file-explorer.md) | Browse + edit workspace files in the web UI |
| [host-terminal.md](foundations/host-terminal.md) | Interactive shell in the web UI (WebSocket + PTY) |
| [windows-support.md](foundations/windows-support.md) | Tier 2 Windows host support |

### Local — Completed

| Spec | Delivers |
|------|----------|
| [desktop-app.md](local/desktop-app.md) | Wails native wrapper (macOS .app, Windows .exe, Linux binary) — archived, code removed 2026-06-14 |
| [terminal-sessions.md](local/terminal-sessions.md) | Multiple concurrent terminal sessions with tab bar |
| [terminal-container-exec.md](local/terminal-container-exec.md) | Attach to running task containers from the terminal panel |
| [oauth-token-setup.md](local/oauth-token-setup.md) | Browser-based OAuth sign-in for Claude and Codex credentials |
| [pixel-agents.md](local/pixel-agents.md) | Pixel art office view — animated characters representing task agents |
| [vue-frontend-migration.md](local/vue-frontend-migration.md) | Converged the vanilla-JS `ui/` board and the Vue `frontend/` site into one Vue 3 + TypeScript SPA with runtime local/cloud mode switching; legacy `ui/` and its build/CI pipeline removed, single embedded `frontend/dist` served by the Go server. Superseded typescript-migration and typed-dom-hooks. |

### Local — Archived (superseded or dropped)

Specs that were never implemented and whose designs target architecture since removed (the vanilla-JS `ui/` frontend, the per-task container model, the Goal field). The still-wanted features were re-specced against the current Vue + host-backend architecture; the dropped one has no replacement.

| Spec | Why archived |
|------|--------------|
| [file-attachments.md](local/file-attachments.md) | Built on `ui/js/tasks.js` and container `-v` mounts (both removed). Replaced by [task-prompt-attachments.md](local/task-prompt-attachments.md). |
| [file-panel-viewer.md](local/file-panel-viewer.md) | Built on `ui/js/explorer.js` (removed). Replaced by [inline-file-panel.md](local/inline-file-panel.md). |
| [inline-diff-feedback.md](local/inline-diff-feedback.md) | Built on `ui/js/modal-diff.js` (removed). Replaced by [diff-review-comments.md](local/diff-review-comments.md). |
| [spatial-canvas.md](local/spatial-canvas.md) | Exploratory infinite-canvas view targeting `ui/js/` (removed). No replacement specced. |
| [local-build-deploy.md](local-build-deploy.md) | Local `make release` / `make deploy` for wallfacerd. Implemented then deliberately reverted in favor of GitHub Actions (`0ba7b225`). |

### Cloud — Archived (superseded by the Latere platform boundary)

Specs proposing wallfacer build infrastructure that Latere services now own. Replaced by the integration track ([latere-integration.md](cloud/latere-integration.md)). Retained for context.

| Spec | Why archived |
|------|--------------|
| [multi-tenant.md](cloud/multi-tenant.md) | Control plane, per-user instance provisioning, routing, and hibernation are owned by **Cella** (runtime) and **terraform** (infra); org/team scoping is Identity's. Wallfacer consumes, not builds. |
| [billing-idempotency.md](cloud/billing-idempotency.md) | Stripe charge mechanics and idempotency are owned by **Identity** (`latere.ai/x/auth`). Wallfacer's only billing surface is an optional read-only subscription UX, to be specced if/when payment is introduced. |
