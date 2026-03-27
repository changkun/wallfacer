# Windows Support — Tier 2 (Native Windows Host)

**Date:** 2026-03-22 (revised 2026-03-27)

## Context

Tier 1 (WSL2) is complete and shipped. Users can run the full Wallfacer stack
inside WSL2 today. This spec covers Tier 2: running the Go server natively on
a Windows host, with Linux sandbox containers launched via Docker Desktop or
Podman Desktop (both use a WSL2 backend internally).

For containerization backend analysis, see `specs/01c-native-sandbox-windows.md`.

## Already Implemented

- Cross-platform signal handling, `execve` replacement, SELinux mount stripping,
  browser/file-manager launch, container runtime detection, path separator handling
- Windows CI: build + vet + unit tests (`.github/workflows/test.yml`)
- Windows release binaries (`windows/amd64` in `.github/workflows/release-binary.yml`;
  `install.sh` supports Windows)
- Native Windows getting-started docs (`docs/guide/getting-started.md`)

## Remaining Work

### A. Container Path Translation

**Status:** Done

### B. End-to-End Testing on Windows

**Status:** Not started
**Effort:** Medium

Current Windows CI (`test-windows` job on `windows-latest`) runs `go build`,
`go vet`, and `go test` only — no container runtime is available. Two test
categories are skipped on Windows: `os.Chmod`-based read-only directory tests
in `tasks_autopilot_test.go` and browser launch tests in `main_test.go`.

**Changes:**
- Write a manual test protocol: task creation, container launch, volume mounts,
  log streaming, commit pipeline, browser launch
- Verify with both Docker Desktop and Podman Desktop
- CI job with Docker Desktop is complex and may not be worth the maintenance;
  keep Windows CI focused on compilation and unit tests

---

## Implementation Order

| Step | Item | Depends on | Effort |
|------|------|------------|--------|
| 1 | Path translation (A) | — | Medium |
| 2 | E2E testing (B) | A | Medium |

Step 1 is the critical remaining work. Step 2 requires A.

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
