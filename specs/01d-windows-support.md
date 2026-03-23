# Windows Support — Tier 2 (Native Windows Host)

**Date:** 2026-03-22 (revised)

## Context

Tier 1 (WSL2) is complete and shipped. Users can run the full Wallfacer stack
inside WSL2 today. This spec covers Tier 2: running the Go server natively on
a Windows host, with Linux sandbox containers launched via Docker Desktop or
Podman Desktop (both use a WSL2 backend internally).

For containerization backend analysis, see `specs/01c-native-sandbox-windows.md`.

## What Tier 1 Already Covers

The following are **done** and do not need further work:

- Cross-platform signal handling (`internal/cli/signal_windows.go`, `signal_unix.go`)
- `execve` replacement on Windows (`internal/cli/execve_windows.go` uses `os/exec.Command`)
- SELinux `:z` mount option stripped on non-Linux (`mountOpts()` in `internal/runner/container.go`)
- Browser launch with WSL2 detection (`openBrowser()` in `internal/cli/cli.go`)
- File manager launch via `explorer.exe` on Windows (`OpenFolder()` in `internal/handler/git.go`)
- Container runtime detection with Windows paths (`detectContainerRuntime()` in `internal/cli/cli.go`)
- `os.TempDir()` used throughout (no hardcoded `/tmp` in production code)
- `sanitizeBasename()` handles both `\` and `/` separators (`internal/runner/util.go`)
- `os.PathListSeparator` for `WALLFACER_WORKSPACES` parsing (`internal/envconfig/envconfig.go`)
- Windows CI job: build + vet + unit tests (`.github/workflows/test.yml`, `test-windows` job)
- WSL2 getting-started docs (`docs/guide/getting-started.md`)
- Path separator notes in `docs/guide/configuration.md` and `docs/guide/workspaces.md`

## Remaining Work

### A. Windows Release Binaries

**Status:** Not started
**Effort:** Small

The release workflow (`.github/workflows/release-binary.yml`) builds for
linux/{amd64,arm64} and darwin/{amd64,arm64}. No Windows target.

**Changes:**
- Add `windows/amd64` to the build matrix in `.github/workflows/release-binary.yml`
  (current matrix: linux/{amd64,arm64}, darwin/{amd64,arm64})
- Output as `wallfacer-windows-amd64.exe` (current naming: `wallfacer-{goos}-{goarch}`)
- Add Windows install instructions (`install.sh` explicitly rejects Windows with
  "Wallfacer supports Linux and macOS"); provide a direct download path or
  document `go install` as the Windows alternative

### B. Container Path Translation

**Status:** Not started
**Effort:** Medium

When the server runs on a Windows host, host paths like `C:\Users\alice\project`
must be translated for container volume mounts. Docker Desktop automatically
translates `C:\` to `/c/`; Podman Desktop expects `/mnt/c/`. The translation
layer must handle both.

**Changes:**
- Add a path translation helper called from `ContainerSpec.Build()` in
  `internal/runner/container_spec.go` (bind mounts already use `--mount`
  syntax at lines 99–114, so colon-in-path ambiguity is not an issue)
- Handle drive letter mapping (`C:` → `/c/` for Docker, `/mnt/c/` for Podman)
- Detect which runtime is in use (the runtime path is available on `ContainerSpec`)
  and apply the correct mapping
- Unit tests for edge cases (UNC paths, spaces, Unicode characters); existing
  tests in `container_spec_test.go` already cover Unicode paths but only with
  Unix-style paths

### C. Build Documentation for Windows

**Status:** Not started
**Effort:** Small (documentation only)

Windows users without `make` need build instructions. The getting-started guide
(`docs/guide/getting-started.md`) currently has a "Windows (WSL2)" section but
no native Windows section. Since the Go server already compiles on Windows,
document `go build -o wallfacer.exe .` as the native Windows build path and add
prerequisites (Go 1.25+, Docker Desktop or Podman Desktop). Defer PowerShell
scripts or task runners until demand appears.

### D. End-to-End Testing on Windows

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
