# Windows Support

**Date:** 2026-03-21

## Problem

Windows users cannot use Wallfacer today. The Go server fails to compile due to
`syscall.SIGTERM` and `syscall.Exec()` usage, and even if patched to compile,
multiple runtime paths assume Unix conventions (hardcoded `/opt/podman/bin/podman`,
SELinux `,z` mount options, `xdg-open` for browser/file-manager launch). A Windows
user who downloads the source and runs `go build` gets a build failure immediately.

This spec covers the full Windows user experience — from installation through daily
use. It does NOT re-analyze containerization backends; see
`specs/native-containerization-windows.md` for that analysis. This spec focuses on:
what changes are needed so a Windows user can `go build`, start the server, and
run tasks.

## Scope

Two support tiers, implemented in order:

| Tier | Description | Target user |
|------|-------------|-------------|
| **Tier 1: WSL2** | Run Wallfacer inside WSL2 with minimal code changes | Any Windows 10 2004+ / Windows 11 user |
| **Tier 2: Native Windows** | Run the Go server on Windows host, containers via Docker Desktop or Podman Desktop | Power users; depends on Claude Code having Windows or WSL2-bridged support |

Tier 1 is the recommended shipping path. Tier 2 is a future option documented here
for completeness.

## Current State: What Works and What Breaks

### Already cross-platform (no changes needed)

- Home directory: `os.UserHomeDir()`
- Workspace path parsing: `filepath.SplitList()` + `os.PathListSeparator`
- Path construction: `filepath.Join()` throughout
- Git operations: `exec.Command("git", ...)`
- Container termination: via runtime CLI (`podman kill`), not OS signals
- Temp files: `os.MkdirTemp()`
- Atomic writes: temp file + rename
- Data storage: all relative to `~/.wallfacer/`

### Compilation blockers (CRITICAL — build fails on Windows)

| # | File | Line(s) | Issue |
|---|------|---------|-------|
| 1 | `internal/cli/server.go` | 137 | `syscall.SIGTERM` — not defined on Windows |
| 2 | `internal/cli/exec.go` | 92, 99 | `syscall.Exec()` — Unix-only; does not exist on Windows |

### Runtime failures (HIGH — server starts but features break)

| # | File | Line(s) | Issue |
|---|------|---------|-------|
| 3 | `internal/cli/cli.go` | 134–145 | `openBrowser()` — `default: return` silently does nothing on Windows |
| 4 | `internal/cli/cli.go` | 113–132 | `detectContainerRuntime()` — probes `/opt/podman/bin/podman`; no Windows paths |
| 5 | `internal/runner/container.go` | 117, 130, 147, 172, 213, 247 | `Options: "z"` and `"z,ro"` — SELinux flag meaningless on non-Linux; may be rejected by Docker Desktop on Windows |
| 6 | `internal/handler/git.go` | 615–621 | `openFolder()` — uses `xdg-open` for all non-macOS; should use `explorer.exe` on Windows |

### Build system (MEDIUM — users cannot `make build`)

| # | File | Issue |
|---|------|-------|
| 7 | `Makefile` | `SHELL := /bin/bash`, hardcoded `/opt/podman/bin/podman`, bash-only scripts |

Items 8–9 below are NOT bugs: sandbox containers always run Linux regardless of
host OS. The Dockerfiles and entrypoint scripts do not need Windows variants.

| # | File | Note |
|---|------|------|
| 8 | `sandbox/claude/Dockerfile` | Ubuntu-based; Linux-only — **correct by design** |
| 9 | `sandbox/*/entrypoint.sh` | Bash-only — **correct by design** (runs inside Linux container) |

---

## Tier 1: WSL2 Support

### Goal

A Windows user can follow a documented path to run Wallfacer inside WSL2 with the
same experience as a native Linux user, including browser launch from WSL2 into
the Windows host browser.

### Required Code Changes

All six changes below are small and isolated. They benefit both Tier 1 and Tier 2.

#### Change 1: Signal handling — `server.go`

**Current:** `signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)`

**Problem:** `syscall.SIGTERM` does not exist on Windows. `os.Interrupt` works
everywhere (maps to `SIGINT` on Unix and `Ctrl+C` on Windows).

**Fix:** Use build tags to select the signal set.

Create `internal/cli/signal_unix.go`:
```go
//go:build !windows

package cli

import (
    "os"
    "syscall"
)

var shutdownSignals = []os.Signal{syscall.SIGTERM, os.Interrupt}
```

Create `internal/cli/signal_windows.go`:
```go
//go:build windows

package cli

import "os"

var shutdownSignals = []os.Signal{os.Interrupt}
```

Update `server.go` line 137:
```go
ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals...)
```

#### Change 2: Replace `syscall.Exec` — `exec.go`

**Current:** `syscall.Exec(runtimePath, execArgs, os.Environ())` replaces the
current process with the container exec for PTY inheritance.

**Problem:** `syscall.Exec` (execve) does not exist on Windows.

**Fix:** Use build tags. On Windows, fall back to `exec.Command` with
stdin/stdout/stderr forwarding and exit code propagation.

Create `internal/cli/execve_unix.go`:
```go
//go:build !windows

package cli

import (
    "os"
    "syscall"
)

func execReplace(binary string, args []string) error {
    return syscall.Exec(binary, args, os.Environ())
}
```

Create `internal/cli/execve_windows.go`:
```go
//go:build windows

package cli

import (
    "os"
    "os/exec"
)

func execReplace(binary string, args []string) error {
    cmd := exec.Command(binary, args[1:]...)
    cmd.Stdin = os.Stdin
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    if err := cmd.Run(); err != nil {
        if exitErr, ok := err.(*exec.ExitError); ok {
            os.Exit(exitErr.ExitCode())
        }
        return err
    }
    os.Exit(0)
    return nil // unreachable
}
```

Update `exec.go` to call `execReplace(runtimePath, execArgs)` instead of the
direct `syscall.Exec(...)` calls.

**Trade-off:** On Windows, `wallfacer exec` creates a child process rather than
replacing itself. PTY resize signals will not propagate automatically. This is
acceptable: Tier 1 WSL2 users run the Linux binary so they get real execve.

#### Change 3: Browser launch + WSL2 detection — `cli.go`

**Current:**
```go
func openBrowser(url string) {
    switch runtime.GOOS {
    case "darwin": cmd = "open"
    case "linux":  cmd = "xdg-open"
    default:       return  // silently does nothing on Windows
    }
}
```

**Fix:** Add Windows case and WSL2 detection:
```go
func isWSL() bool {
    return os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != ""
}

func openBrowser(url string) {
    switch runtime.GOOS {
    case "darwin":
        exec.Command("open", url).Start()
    case "windows":
        exec.Command("cmd", "/c", "start", url).Start()
    case "linux":
        if isWSL() {
            exec.Command("cmd.exe", "/c", "start", url).Start()
        } else {
            exec.Command("xdg-open", url).Start()
        }
    }
}
```

#### Change 4: Container runtime detection — `cli.go`

**Current:** Checks `/opt/podman/bin/podman` then `LookPath("podman")` then
`LookPath("docker")`.

**Fix:** Skip the Unix-only path on Windows; add Windows-specific install
locations:
```go
func detectContainerRuntime() string {
    if override := strings.TrimSpace(os.Getenv("CONTAINER_CMD")); override != "" {
        return override
    }
    // Unix: preferred explicit podman installation.
    if runtime.GOOS != "windows" {
        if _, err := os.Stat("/opt/podman/bin/podman"); err == nil {
            return "/opt/podman/bin/podman"
        }
    }
    // Windows: check common install locations.
    if runtime.GOOS == "windows" {
        for _, candidate := range []string{
            filepath.Join(os.Getenv("ProgramFiles"), "RedHat", "Podman", "podman.exe"),
        } {
            if _, err := os.Stat(candidate); err == nil {
                return candidate
            }
        }
    }
    // Cross-platform: podman on $PATH.
    if p, err := exec.LookPath("podman"); err == nil {
        return p
    }
    // Cross-platform: docker on $PATH.
    if p, err := exec.LookPath("docker"); err == nil {
        return p
    }
    if runtime.GOOS == "windows" {
        return "podman.exe"
    }
    return "/opt/podman/bin/podman"
}
```

#### Change 5: SELinux mount options — `container.go`

**Current:** All `VolumeMount` entries use `Options: "z"` or `Options: "z,ro"`.
The `z` flag is SELinux-specific and only meaningful on Linux hosts with SELinux
enabled. Docker Desktop on Windows and macOS ignores or rejects it.

**Fix:** Introduce a helper that strips the `z` option on non-Linux hosts:
```go
// mountOpts returns volume mount options appropriate for the host OS.
// The "z" SELinux relabeling option is only included on Linux.
func mountOpts(opts ...string) string {
    if runtime.GOOS != "linux" {
        filtered := make([]string, 0, len(opts))
        for _, o := range opts {
            if o != "z" {
                filtered = append(filtered, o)
            }
        }
        return strings.Join(filtered, ",")
    }
    return strings.Join(opts, ",")
}
```

Replace all hardcoded `"z"` with `mountOpts("z")` and `"z,ro"` with
`mountOpts("z", "ro")` across `container.go`, `refine.go`, `ideate.go`, and
`exec.go`.

**Bonus:** This also fixes macOS Docker Desktop users who may hit the same issue.

#### Change 6: File manager launch — `git.go`

**Current:**
```go
switch runtime.GOOS {
case "darwin": cmd = exec.CommandContext(r.Context(), "open", req.Path)
default:       cmd = exec.CommandContext(r.Context(), "xdg-open", req.Path)
}
```

**Fix:** Add Windows case:
```go
switch runtime.GOOS {
case "darwin":
    cmd = exec.CommandContext(r.Context(), "open", req.Path)
case "windows":
    cmd = exec.CommandContext(r.Context(), "explorer", req.Path)
default:
    cmd = exec.CommandContext(r.Context(), "xdg-open", req.Path)
}
```

### Documentation for Tier 1

Add a Windows/WSL2 section to `docs/guide/getting-started.md`:

1. **Install WSL2:** `wsl --install` (requires Windows 10 2004+ or Windows 11)
2. **Inside WSL2**, install Go 1.25+ and Podman (or Docker Engine)
3. **Clone repo into the WSL2 filesystem** (not `/mnt/c/` — much slower due to
   cross-filesystem overhead)
4. `go build -o wallfacer . && ./wallfacer run ~/project`
5. Browser opens automatically via `cmd.exe /c start`
6. Note: keep workspace repos on the WSL2 filesystem for performance

### Testing for Tier 1

- **CI:** Add a GitHub Actions job with `runs-on: windows-latest` that:
  1. Runs `go build ./...` to verify compilation succeeds on Windows
  2. Runs `go vet ./...`
  3. Runs `go test ./...` (unit tests only — no container runtime available)
- **Manual:** Test WSL2 browser launch, runtime detection, and a full task cycle
  inside WSL2 on a Windows machine

---

## Tier 2: Native Windows (Future)

### Prerequisites

- All six code changes from Tier 1 (already done)
- Claude Code must support Windows — either natively or via a WSL2 bridge where
  the container runtime runs Linux containers through Docker Desktop's WSL2 backend

### Additional changes needed

#### Container path translation

When the Go server runs on Windows host but containers run Linux (via Docker
Desktop WSL2 backend), host paths like `C:\Users\alice\project` must be translated
to `/mnt/c/Users/alice/project` for container volume mounts. This requires a path
translation layer in `ContainerSpec.Build()`.

#### Makefile alternative

Windows users without `make` need an alternative. Options:

1. **Document `go build` directly** — the Makefile is mostly convenience;
   `go build -o wallfacer.exe .` works on Windows (once compilation is fixed)
2. **PowerShell script** (`build.ps1`) — lower friction for Windows users
3. **`go run` task runner** — cross-platform, no extra tools

Recommendation: Option 1 for Tier 1. Option 2 only if Windows usage grows.

#### Windows service support

For long-running server use, Wallfacer could register as a Windows Service.
Out of scope for initial Windows support but noted for future work.

---

## Implementation Order

### Phase 1: Compile and WSL2 (Tier 1)

| Step | Change | File(s) | Effort |
|------|--------|---------|--------|
| 1 | Signal handling build tags | `internal/cli/signal_unix.go` (new), `signal_windows.go` (new), `server.go` | Small |
| 2 | `syscall.Exec` build tags | `internal/cli/execve_unix.go` (new), `execve_windows.go` (new), `exec.go` | Small |
| 3 | Browser launch + WSL2 detection | `internal/cli/cli.go` | Small |
| 4 | Container runtime detection | `internal/cli/cli.go` | Small |
| 5 | SELinux mount option stripping | `internal/runner/container.go`, `refine.go`, `ideate.go`, `internal/cli/exec.go` | Small |
| 6 | File manager launch | `internal/handler/git.go` | Small |
| 7 | Windows CI job | `.github/workflows/` | Small |
| 8 | WSL2 getting-started docs | `docs/guide/getting-started.md` | Small |
| 9 | Update `CLAUDE.md` and `AGENTS.md` | Root-level docs | Small |

### Phase 2: Native Windows (Tier 2) — future

| Step | Change | Effort |
|------|--------|--------|
| 10 | Path translation for Docker Desktop WSL2 backend | Medium |
| 11 | PowerShell build script or task runner | Medium |
| 12 | End-to-end testing on Windows host + Docker Desktop | Medium |
| 13 | Windows service support | Large |

---

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Claude Code does not support Windows natively | Tier 1 (WSL2) sidesteps this — Claude Code runs in a Linux container |
| Docker Desktop license restrictions for commercial use | Podman Desktop is free; document both options |
| SELinux option removal breaks Linux hosts | `mountOpts` only strips `z` on non-Linux; Linux behavior unchanged |
| `syscall.Exec` replacement loses PTY on Windows | Only affects `wallfacer exec` on native Windows; WSL2 users run Linux binary |
| Windows CI adds maintenance burden | Keep it build + vet + unit-test only; no container integration tests |

## Non-Goals

- Windows ARM64 support (revisit if demand appears)
- Native Windows containers (Claude Code is Linux-only)
- GUI installer / MSI package (premature; `go build` or pre-built binary suffices)
- Windows-specific sandbox images (containers are always Linux)
