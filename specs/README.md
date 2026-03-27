# Specs

Implementation roadmap for wallfacer. Numbered specs (`01-`–`08-`) form the cloud/platform milestone sequence. Unnumbered specs are core improvements independent of cloud deployment.

## Core Infrastructure

| Spec | Status | Delivers |
|------|--------|----------|
| [epic-coordination.md](epic-coordination.md) | Not started | Planner tasks (spec → tasks), dependency-aware board.json, gate tasks, epic progress tracking. UX depends on M4 (file explorer) for spec browsing and chat-driven iteration. |

## Cloud/Platform Milestone Graph

```
                                ┌──▶ M3: Container Reuse
                                │
M1: Sandbox Backend Interface ──┼──▶ M6: Cloud Backends ──▶ M8: Multi-Tenant
                                │           ▲                     (capstone)
                                │   M2: Storage Interface ─┤
                                │           │               │
                                │           └──▶ M2.5: Multi-Workspace Groups
                                │
                                ├──▶ Native Containerization (platform-specific)
                                │     ├─ Linux  (bubblewrap, systemd-nspawn)
                                │     ├─ macOS  (Virtualization.framework, sandbox_init)
                                │     └─ Windows (Job Objects, Hyper-V)
                                │
M4: File Explorer (local) ──────┼────────────────────────▶│ (Phase 4)
        │                       │
        └──▶ Epic Coordination (spec management UX)
                                │
M5: Host Terminal (local) ──────┼────────────────────────▶│ (Phase 3)
M7: Desktop App ────────────────┘ (ships after UX)
```

## Milestones

| # | Milestone | Spec | Status | Delivers |
|---|-----------|------|--------|----------|
| **M1** | Sandbox backend interface | [01-sandbox-backends.md](01-sandbox-backends.md) | **Complete** | `sandbox.Backend`/`sandbox.Handle` + `LocalBackend` |
| **M2** | Storage backend interface | [02-storage-backends.md](02-storage-backends.md) | **Enablers complete** | `StorageBackend` + `FilesystemBackend` + `ListBlobs`; cloud backends (PG, S3) deferred |
| **M3** | Container reuse | [03-container-reuse.md](03-container-reuse.md) | Not started | Aux worker containers for title/oversight/commit (~10x startup savings) |
| **M4** | File explorer | [04-file-explorer.md](04-file-explorer.md) | Not started | Browse + edit workspace files in the web UI |
| **M5** | Host terminal | [05-host-terminal.md](05-host-terminal.md) | Not started | Interactive shell in the web UI (WebSocket + PTY) |
| **M6** | Cloud backends | [06-cloud-backends.md](06-cloud-backends.md) | Not started | K8s backend, PostgreSQL, S3, migration tool |
| **M7** | Desktop app | [07-native-desktop-app.md](07-native-desktop-app.md) | Not started | Wails native wrapper (macOS .app, Windows .exe) |
| **M8** | Multi-tenant (capstone) | [08-cloud-multi-tenant.md](08-cloud-multi-tenant.md) | Not started | Control plane, auth, instance lifecycle, cloud file/terminal access |

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
| [02a-multi-workspace-groups.md](02a-multi-workspace-groups.md) | Not started | Multi-store manager, runner task-to-group mapping. Run tasks across workspace groups simultaneously. |

After M2 (store interfaces stable). Independent of M3. Can run in parallel with M3.

## Related to M4 — File Attachments & Host Mounts

| Spec | Status | Delivers |
|------|--------|----------|
| [04a-file-image-attachments.md](04a-file-image-attachments.md) | Not started | Drag-and-drop file and image attachments for task prompts |
| [04b-host-mounts.md](04b-host-mounts.md) | Not started | Per-task read-only host filesystem mounts into sandbox containers |

## Independent Enhancements

| Spec | Status | Delivers |
|------|--------|----------|
| [90-oversight-risk-scoring.md](90-oversight-risk-scoring.md) | Not started | Real-time agent action risk assessment |
| [91-visual-verification.md](91-visual-verification.md) | Not started | Visual verification for UI changes |
| [92-live-serve.md](92-live-serve.md) | Not started | Build and run developed software from within Wallfacer |
| [93-agent-abstraction.md](93-agent-abstraction.md) | Not started | Agent role abstraction, pluggable role descriptors, multi-agent communication |

## Ordering Rationale

- **Epic coordination depends on M4 (file explorer)** for its spec management UX. The backend pieces (planner task kind, board.json context, gate tasks) are independent, but the full UX — browsing specs, focused markdown view, chat-driven iteration — requires the file explorer panel. Implement M4 Phase 1 first, then epic coordination.
- **M1–M2 first:** Pure refactors creating abstraction seams all downstream milestones plug into. Low risk, high leverage.
- **M3 after M1:** Container reuse modifies the same `internal/runner/` files. Doing it right after M1 avoids revisiting them later.
- **M2.5 after M2:** Multi-workspace groups modifies store lifecycle; wait for `StorageBackend` interfaces to stabilize. Can run in parallel with M3.
- **M4–M5 before M6:** Deliver user-visible value with no cloud dependency. Exercise different code paths (`internal/handler/` + `ui/`).
- **M7 after M4–M5:** Desktop app ships with file explorer + terminal already built in. Fully independent — can move earlier.
- **M8 last:** Capstone wiring everything together. Picks up deferred cloud phases from M3/M4/M5.
