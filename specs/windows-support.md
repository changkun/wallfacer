# Windows Support

**Date:** 2026-03-22 (revised)

## Problem

Native Windows support — running the Go server directly on the Windows host —
depends on Claude Code having a Windows-compatible story. WSL2 support (Tier 1)
is complete; this spec covers the remaining work for native Windows (Tier 2).

For containerization backend analysis, see `specs/native-containerization-windows.md`.

## Prerequisites

- WSL2 support (Tier 1) is complete: signal handling, execve build tags,
  browser/WSL2 detection, runtime detection, SELinux mount stripping, file
  manager launch, Windows CI, docs, and compat tests are all shipped.
- Claude Code must support Windows natively or via a WSL2 bridge where the
  container runtime runs Linux containers through Docker Desktop's WSL2 backend

### 2A: Windows Release Binaries

**Status:** Not started

The release workflow (`.github/workflows/release-binary.yml`) currently builds
for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64. No Windows targets.

**Changes needed:**
- Add `windows/amd64` (and optionally `windows/arm64`) to the build matrix
- Output binary as `wallfacer-windows-amd64.exe` (`.exe` suffix)
- Update `install.sh` or add a separate install mechanism for Windows (the
  current script explicitly rejects Windows)

**Effort:** Small

### 2B: Container Path Translation

**Status:** Not started

When the Go server runs on a Windows host but containers run Linux (via Docker
Desktop's WSL2 backend), host paths like `C:\Users\alice\project` must be
translated to `/mnt/c/Users/alice/project` for container volume mounts.

**Changes needed:**
- Add a path translation layer in `ContainerSpec.Build()` that converts Windows
  host paths to WSL2/Docker Desktop mount paths
- Handle drive letter mapping (`C:` → `/mnt/c/`, `D:` → `/mnt/d/`)
- Unit tests for path conversion edge cases (UNC paths, spaces, Unicode)

**Effort:** Medium

### 2C: Makefile Alternative

**Status:** Not started

Windows users without `make` need a build alternative. The Makefile uses
`SHELL := /bin/bash` and bash-specific syntax.

**Options (in priority order):**
1. **Document `go build` directly** — `go build -o wallfacer.exe .` works once
   compilation is fixed (already true). Sufficient for now.
2. **PowerShell script** (`build.ps1`) — only if Windows usage grows
3. **Cross-platform task runner** — only if maintaining two build systems
   becomes a burden

**Recommendation:** Document `go build` as the Windows build path. Defer
scripts until there is demand.

**Effort:** Small (documentation only)

### 2D: End-to-End Testing on Windows

**Status:** Not started

Current Windows CI only runs `go build`, `go vet`, and unit tests. No
container runtime is available in the CI environment.

**Changes needed:**
- Manual test protocol for Windows host + Docker Desktop
- Verify: task creation, container launch, volume mounts, log streaming,
  commit pipeline, browser launch
- Optionally: CI job with Docker Desktop (complex setup, may not be worth it)

**Effort:** Medium

### 2E: Windows Service Support (Deferred)

**Status:** Not planned

For long-running server use, Wallfacer could register as a Windows Service.
Out of scope until there is demonstrated demand for native Windows deployment.

**Effort:** Large

---

## Implementation Order for Tier 2

| Step | Change | Depends on | Effort |
|------|--------|------------|--------|
| 1 | Windows release binaries (2A) | — | Small |
| 2 | Document `go build` for Windows (2C) | — | Small |
| 3 | Container path translation (2B) | Claude Code Windows support | Medium |
| 4 | End-to-end testing (2D) | 2B | Medium |
| 5 | Windows service support (2E) | Demand | Large |

Steps 1–2 can be done immediately. Steps 3–4 are blocked on Claude Code
having a Windows-compatible execution path.

---

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Claude Code does not support Windows natively | WSL2 support is complete and sidesteps this |
| Docker Desktop license restrictions for commercial use | Podman Desktop is free; document both options |
| Path translation edge cases on Windows | Thorough unit tests for drive letters, UNC, Unicode, spaces |
| Windows CI adds maintenance burden | Keep minimal: build + vet + unit tests only |

## Non-Goals

- Windows ARM64 support (revisit if demand appears)
- Native Windows containers (Claude Code is Linux-only)
- GUI installer / MSI package (premature; `go build` or pre-built binary suffices)
- Windows-specific sandbox images (containers are always Linux)
