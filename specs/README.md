# Specs

Implementation roadmap for wallfacer. Numbered specs (`01-`–`08-`) form the cloud/platform milestone sequence. Unnumbered specs are core improvements independent of cloud deployment.

## Core Infrastructure

| Spec | Status | Delivers |
|------|--------|----------|
| [epic-coordination.md](epic-coordination.md) | Not started | Planner tasks (spec → tasks), dependency-aware board.json, gate tasks, epic progress tracking. UX depends on M4 (file explorer) for spec browsing and chat-driven iteration. |

## Status Quo

What has shipped vs what remains. Items marked ✅ are complete; ○ are not started.

```
✅ M1: Sandbox Backend Interface
│
├──▶ ✅ M1d: Windows Support
├──▶ ○  M1a: Native Sandbox (Linux)
├──▶ ○  M1b: Native Sandbox (macOS)
├──▶ ○  M1c: Native Sandbox (Windows)
│
├──▶ ✅ M2: Storage Backend Interface (enablers complete)
│    ├──▶ ✅ M2a: Multi-Workspace Groups
│    └──▶ ○  M2 cloud tasks (PG, S3 — deferred in M2 spec)
│
├──▶ ✅ M3: Container Reuse (core)
│    └──▶ ○  M3a: Overlay Snapshots / CRIU
│
├── next ──────────────────────────
│
├──▶ ✅ M4: File Explorer
├──▶ ✅ M5: Host Terminal
│    ├──▶ ○  M5a: Terminal Sessions (tabs)
│    └──▶ ○  M5b: Container Exec
├──▶ ○  M6: Cloud Deployment (overview)
│    ├──▶ ○  M6a: Tenant Filesystem
│    ├──▶ ○  M6b: K8s Sandbox Backend
│    └──▶ ○  M6c: Cloud Infrastructure (IaC per provider)
├──▶ ○  M7: Desktop App
├──▶ ○  M8a: Authentication (OAuth/OIDC login, sessions, identity)
├──▶ ○  M8: Multi-Tenant (capstone, requires M8a)
└──▶ ○  M8b: Tenant API (external API, webhooks; requires M8a + M8)

○  Epic Coordination (blocked on M4)
○  Independent: 90–93 (oversight, visual, live-serve, agent abstraction)
○  Attachments: 04a (file/image), 04b (host mounts)
```

## Overall Plan

Full milestone dependency graph showing how everything relates.

```
                                 ┌──▶ M3: Container Reuse ──▶ M3a: Overlay Snapshots / CRIU
                                 │
 M1: Sandbox Backend Interface ──┼──▶ M6a: Tenant Filesystem ──▶ M6b: K8s Sandbox ──┐
                                 │            ▲                                      │
                                 │    M2: Storage Interface ─┐                       ├──▶ M8: Multi-Tenant
                                 │            │              │                       │       (capstone)
                                 │            ├──▶ M2a: Multi-Workspace Groups       │
                                 │            └──▶ M2 cloud (PG, S3) ───────────────┤
                                 │                                                   │
                                 │    M8a: Authentication (OAuth, sessions) ─────────┤
                                 │                                                   │
                                 │    M6c: Cloud Infra (IaC per provider) ───────────┘
                                 │                                                   │
                                 │                                              M8b: Tenant API
                                 │                                         (external API, webhooks)
                                 │      (DO, AWS, GCP, Alibaba, self-hosted)
                                 │
                                 ├──▶ Native Containerization (platform-specific)
                                 │     ├─ M1a: Linux  (bubblewrap, systemd-nspawn)
                                 │     ├─ M1b: macOS  (Virtualization.framework, sandbox_init)
                                 │     ├─ M1c: Windows (Job Objects, Hyper-V)
                                 │     └─ M1d: Windows Support (tier 2 host)
                                 │
 M4: File Explorer (local) ──────┼────────────────────────▶│ (Phase 4)
         │                       │
         ├──▶ Epic Coordination (spec management UX)
         ├──▶ 04a: File/Image Attachments
         └──▶ 04b: Host Mounts
                                 │
 M5: Host Terminal (local) ──────┼────────────────────────▶│ (Phase 3)
         │
         ├──▶ 05a: Terminal Sessions (tabs)
         └──▶ 05b: Container Exec
                                 │
 M7: Desktop App ────────────────┘ (ships after M4+M5 UX)

 Independent (no milestone deps)
   ├─ 90: Oversight Risk Scoring
   ├─ 91: Visual Verification
   ├─ 92: Live Serve
   └─ 93: Agent Abstraction
```

**Cloud deployment dependency chain:**
```
M2 (storage interface) ──▶ M6a (tenant filesystem) ──▶ M6b (K8s sandbox) ──▶ M8 (multi-tenant)
M1 (sandbox interface) ─────────────────────────────▶ M6b                        ▲
M2 (storage interface) ──▶ M2 cloud (PG, S3) ───────────────────────────────────┤
M8a (authentication) ───────────────────────────────────────────────────────────┤
M6c (cloud infra: DO, AWS, GCP, Alibaba, self-hosted IaC) ─────────────────────┘
                                                                                │
M8a (authentication) + M8 (multi-tenant) ──▶ M8b: Tenant API (external API, webhooks)
```

## Milestones

| # | Milestone | Spec | Status | Delivers |
|---|-----------|------|--------|----------|
| **M1** | Sandbox backend interface | [01-sandbox-backends.md](01-sandbox-backends.md) | **Complete** | `sandbox.Backend`/`sandbox.Handle` + `LocalBackend` |
| **M2** | Storage backend interface | [02-storage-backends.md](02-storage-backends.md) | **Enablers complete** | `StorageBackend` + `FilesystemBackend` + `ListBlobs`; cloud backends (PG, S3) deferred |
| **M3** | Container reuse | [03-container-reuse.md](03-container-reuse.md) | **Complete** (core) | Per-task worker containers via `podman exec`; ~10x startup savings per turn |
| **M4** | File explorer | [04-file-explorer.md](04-file-explorer.md) | **Complete** | Browse + edit workspace files in the web UI |
| **M5** | Host terminal | [05-host-terminal.md](05-host-terminal.md) | **Complete** | Interactive shell in the web UI (WebSocket + PTY) |
| **M6** | Cloud deployment | [06-cloud-backends.md](06-cloud-backends.md) | Not started | Overview: VPS recipe (done), per-user instance architecture, sub-milestone index |
| **M7** | Desktop app | [07-native-desktop-app.md](07-native-desktop-app.md) | Not started | Wails native wrapper (macOS .app, Windows .exe) |
| **M8** | Multi-tenant (capstone) | [08-cloud-multi-tenant.md](08-cloud-multi-tenant.md) | Not started | Control plane, instance provisioning and lifecycle (auth via M8a) |

## Branches from M8 — Authentication & Tenant API

| Spec | Status | Delivers |
|------|--------|----------|
| [08a-authentication.md](08a-authentication.md) | Not started | OAuth2/OIDC login (GitHub, Google, generic), session management, user identity model, trusted proxy mode for M8 |
| [08b-tenant-api.md](08b-tenant-api.md) | Not started | Versioned external API (`/api/v1/`), per-tenant API keys, webhooks, rate limiting. Programmatic access for CI/CD and scripting. |

M8a: Implement before M8. Also independently useful for single-host deployments (replaces static API key with real login).
M8b: Implement after M8 control plane exists. Start with API key auth + task CRUD, then add webhooks.

## Branches from M1 — Native Sandbox Backends

Alternative `SandboxBackend` implementations. Independent of each other and of M2–M8.

| Spec | Status | Delivers |
|------|--------|----------|
| [01a-native-sandbox-linux.md](01a-native-sandbox-linux.md) | Not started | `BubblewrapBackend`, `NspawnBackend` — daemon-free, zero-install on most distros |
| [01b-native-sandbox-macos.md](01b-native-sandbox-macos.md) | Not started | `VZBackend` (Virtualization.framework), `SandboxInitBackend` (sandbox_init) |
| [01c-native-sandbox-windows.md](01c-native-sandbox-windows.md) | Not started | `JobObjectBackend`, `HyperVBackend` — native Windows isolation |
| [01d-windows-support.md](01d-windows-support.md) | **Complete** | Tier 2 Windows host support (release binaries, path translation, docs) |

## Branch from M2 — Multi-Workspace Groups

| Spec | Status | Delivers |
|------|--------|----------|
| [02a-multi-workspace-groups.md](02a-multi-workspace-groups.md) | **Complete** | Multi-store manager, runner task-to-group mapping. Run tasks across workspace groups simultaneously. |

After M2 (store interfaces stable). Independent of M3. Can run in parallel with M3.

## Branch from M3 — Overlay Snapshots

| Spec | Status | Delivers |
|------|--------|----------|
| [03a-overlay-snapshots.md](03a-overlay-snapshots.md) | Not started | Overlay snapshot cloning for warm worker creation, CRIU checkpoint/restore for sync acceleration |

After M3 (per-task workers complete). Independent of M4–M8.

## Related to M4 — File Attachments & Host Mounts

| Spec | Status | Delivers |
|------|--------|----------|
| [04a-file-image-attachments.md](04a-file-image-attachments.md) | Not started | Drag-and-drop file and image attachments for task prompts |
| [04b-host-mounts.md](04b-host-mounts.md) | Not started | Per-task read-only host filesystem mounts into sandbox containers |
| [04c-file-panel-viewer.md](04c-file-panel-viewer.md) | Not started | VS Code-style inline file panel with tabs, multi-modal preview (images, video, PDF) |

## Branches from M5 — Terminal Extensions

| Spec | Status | Delivers |
|------|--------|----------|
| [05a-terminal-sessions.md](05a-terminal-sessions.md) | Not started | Multiple concurrent terminal sessions with a tab bar |
| [05b-terminal-container-exec.md](05b-terminal-container-exec.md) | Not started | Attach to running task containers from the terminal panel |

## Branches from M6 — Cloud Deployment

| Spec | Status | Delivers |
|------|--------|----------|
| [06a-tenant-filesystem.md](06a-tenant-filesystem.md) | Not started | Per-tenant persistent volume, repo provisioner (clone/fetch/creds), workspace group cloud mapping, config persistence across hibernate/wake |
| [06b-k8s-sandbox.md](06b-k8s-sandbox.md) | Not started | `K8sBackend` implementing `sandbox.Backend` — K8s Jobs with PVC mounts, pod log streaming, exec |
| [06c-cloud-infrastructure.md](06c-cloud-infrastructure.md) | Not started | Per-provider IaC modules (DO, AWS, GCP, Alibaba, self-hosted), base K8s manifests, deployment docs |

M6a depends on M1 + M2. M6b depends on M1 + M6a. M6c depends on M6a + M6b + M2 cloud + M8. All feed into M8.

## Independent Enhancements

| Spec | Status | Delivers |
|------|--------|----------|
| [90-oversight-risk-scoring.md](90-oversight-risk-scoring.md) | Not started | Real-time agent action risk assessment |
| [91-visual-verification.md](91-visual-verification.md) | Not started | Visual verification for UI changes |
| [92-live-serve.md](92-live-serve.md) | Not started | Build and run developed software from within Wallfacer |
| [93-agent-abstraction.md](93-agent-abstraction.md) | Not started | Agent role abstraction, pluggable role descriptors, multi-agent communication |

## Deployment Scaling Strategy

Two modes, no intermediate step:

1. **VPS (today):** Single VM, single user, filesystem storage, local containers. ~$48–96/mo on DO. This is the development and personal environment.
2. **K8s (when scaling):** Go straight to managed K8s. Each tenant gets a wallfacer pod + PVC. Task containers dispatch as K8s Jobs on shared nodes. Validated by running yourself as tenant #1 on the cluster.

**Why no VM-per-tenant intermediate?** The wallfacer binary is identical in both modes. Building a VM provisioner for the control plane then replacing it with K8s pod provisioning is wasted work. On DO, DOKS control plane is free — the cost premium over VPS is ~$32/mo (managed PG + Spaces + LB). See [06-cloud-backends.md](06-cloud-backends.md) for full cost estimates.

## Ordering Rationale

- **Epic coordination depends on M4 (file explorer)** for its spec management UX. The backend pieces (planner task kind, board.json context, gate tasks) are independent, but the full UX — browsing specs, focused markdown view, chat-driven iteration — requires the file explorer panel. Implement M4 Phase 1 first, then epic coordination.
- **M1–M2 first:** Pure refactors creating abstraction seams all downstream milestones plug into. Low risk, high leverage.
- **M3 after M1:** Container reuse modifies the same `internal/runner/` files. Doing it right after M1 avoids revisiting them later.
- **M2a after M2:** Multi-workspace groups modifies store lifecycle; wait for `StorageBackend` interfaces to stabilize. Can run in parallel with M3.
- **M4–M5 before M6:** Deliver user-visible value with no cloud dependency. Exercise different code paths (`internal/handler/` + `ui/`).
- **M6a before M6b:** Tenant filesystem is the foundation — repos, worktrees, and config must have a cloud home before K8s can mount them into pods.
- **M6b after M6a:** K8s sandbox backend consumes the tenant volume layout. Without M6a, there's nothing to mount.
- **M2 cloud tasks parallel with M6a/M6b:** Task data storage (PG + S3) is independent of the filesystem layer. Can be built concurrently.
- **M7 after M4–M5:** Desktop app ships with file explorer + terminal already built in. Fully independent — can move earlier.
- **M6c after M6a+M6b+M8:** Cloud infrastructure IaC is a leaf — it provisions the managed services that the application layer consumes. Can draft modules early but end-to-end testing requires all cloud milestones. DO first (primary target), then self-hosted, then enterprise clouds.
- **M8a before M8:** Authentication is independently useful (replaces static API key) and required by M8.
- **M8 last:** Capstone wiring M6a (tenant FS) + M6b (K8s sandbox) + M2 cloud (PG/S3) + M8a (auth) + control plane (provisioning).
