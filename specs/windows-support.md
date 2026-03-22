# Windows Support — Tier 2 (Native Windows Host)

**Date:** 2026-03-22 (revised)

## Context

Tier 1 (WSL2) is complete and shipped. Users can run the full Wallfacer stack
inside WSL2 today. This spec covers Tier 2: running the Go server natively on
a Windows host, with Linux sandbox containers launched via Docker Desktop or
Podman Desktop (both use a WSL2 backend internally).

For containerization backend analysis, see `specs/native-containerization-windows.md`.

## What Tier 1 Already Covers

The following are **done** and do not need further work:

- Cross-platform signal handling (`signal_windows.go`, `signal_unix.go`)
- `execve` replacement on Windows (`execve_windows.go` uses `cmd.Run()`)
- SELinux `:z` mount option stripped on non-Linux (`mountOpts()` in `container.go`)
- Browser launch with WSL2 detection (`openBrowser()` in `cli.go`)
- File manager launch via `explorer.exe` on Windows (`git.go`)
- Container runtime detection with Windows paths (`detectContainerRuntime()`)
- `os.TempDir()` used throughout (no hardcoded `/tmp` in production code)
- Windows CI job: build + vet + unit tests (`.github/workflows/test.yml`)
- WSL2 getting-started docs (`docs/guide/getting-started.md`)

## Remaining Work

### A. Windows Release Binaries

**Status:** Not started
**Effort:** Small

The release workflow (`.github/workflows/release-binary.yml`) builds for
linux/{amd64,arm64} and darwin/{amd64,arm64}. No Windows target.

**Changes:**
- Add `windows/amd64` to the build matrix
- Output as `wallfacer-windows-amd64.exe`
- Add Windows install instructions (the current `install.sh` rejects Windows)

### B. Container Path Translation

**Status:** Not started
**Effort:** Medium

When the server runs on a Windows host, host paths like `C:\Users\alice\project`
must be translated for container volume mounts. Docker Desktop automatically
translates `C:\` to `/c/`; Podman Desktop expects `/mnt/c/`. The translation
layer must handle both.

**Changes:**
- Add path translation in `ContainerSpec.Build()` for Windows host paths
- Handle drive letter mapping (`C:` → `/c/` for Docker, `/mnt/c/` for Podman)
- Detect which runtime is in use and apply the correct mapping
- Unit tests for edge cases (UNC paths, spaces, Unicode characters)

### C. Build Documentation for Windows

**Status:** Not started
**Effort:** Small (documentation only)

Windows users without `make` need build instructions. Since the Go server
already compiles on Windows, document `go build -o wallfacer.exe .` as the
Windows build path. Defer PowerShell scripts or task runners until demand
appears.

### D. End-to-End Testing on Windows

**Status:** Not started
**Effort:** Medium

Current Windows CI only runs build, vet, and unit tests — no container runtime
is available.

**Changes:**
- Write a manual test protocol: task creation, container launch, volume mounts,
  log streaming, commit pipeline, browser launch
- Verify with both Docker Desktop and Podman Desktop
- CI job with Docker Desktop is complex and may not be worth the maintenance

### E. Windows Service Support

**Status:** Deferred
**Effort:** Large

Register Wallfacer as a Windows Service for long-running deployments. Out of
scope until there is demonstrated demand.

---

## Implementation Order

| Step | Item | Depends on | Effort |
|------|------|------------|--------|
| 1 | Release binaries (A) | — | Small |
| 2 | Build docs (C) | — | Small |
| 3 | Path translation (B) | — | Medium |
| 4 | E2E testing (D) | B | Medium |
| 5 | Windows service (E) | Demand | Large |

Steps 1–3 are independent. Step 4 requires B.

## Risks

| Risk | Mitigation |
|------|------------|
| Docker Desktop commercial license restrictions | Document Podman Desktop as the free alternative |
| Path translation differs between Docker and Podman | Detect runtime and apply correct mapping; thorough unit tests |
| Windows CI maintenance burden | Keep minimal: build + vet + unit tests only |

## Non-Goals

- Windows ARM64 support (revisit if demand appears)
- Native Windows containers (Claude Code requires Linux)
- GUI installer / MSI package (`go build` or pre-built binary suffices)
- Windows-specific sandbox images (containers are always Linux)
