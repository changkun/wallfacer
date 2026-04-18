---
title: "Native Containerization: macOS"
status: archived
depends_on:
  - specs/foundations/sandbox-backends.md
affects: [internal/sandbox/]
effort: large
created: 2026-03-13
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---


# Native Containerization: macOS

## Problem

Wallfacer currently requires Docker or Podman to execute tasks. On macOS, both carry
significant friction: Docker Desktop requires a commercial licence for teams, and
Podman requires a separate VM daemon (`podman machine`). A native alternative would
lower the barrier to entry and remove the daemon dependency entirely.

The original spec explored external CLI tools (`apple/container`, `vfkit`, `sandbox-exec`,
Lima). This revised spec focuses on **pure Go implementations** — i.e., replacing the
`LocalBackend` (which shells out to an external binary) with Go code that drives
macOS platform APIs directly. No external container runtime binary is required at
runtime; the isolation logic is compiled into the Wallfacer binary.

## Scope

This spec covers macOS-only isolation techniques that can replace `LocalBackend`
as alternative `sandbox.Backend` implementations (see [sandbox-backends.md](../foundations/sandbox-backends.md)).

**Prerequisite:** [sandbox-backends.md](../foundations/sandbox-backends.md) — **complete** (v0.0.6).
Each native backend implements `sandbox.Backend` (`internal/sandbox/`), receiving
a structured `sandbox.ContainerSpec` via `Launch(ctx, spec)`. The returned
`sandbox.Handle` tracks lifecycle states and streams output via `Stdout()`/`Stderr()`.

```go
// internal/sandbox/backend.go
type Backend interface {
    Launch(ctx context.Context, spec ContainerSpec) (Handle, error)
    List(ctx context.Context) ([]ContainerInfo, error)
}
```

---

## Why Pure Go?

| Concern | CLI approach | Pure Go approach |
|---|---|---|
| External dependency | Requires `podman`, `docker`, or `container` binary | None beyond the Wallfacer binary itself |
| Lifecycle management | Orphan containers possible on crash | VM/process owned by the Go process; cleaned up via `defer` |
| Context cancellation | Kill via separate subprocess | `vm.RequestStop()` / `cmd.Cancel()` directly |
| Error handling | Exit code guessing; text matching | Typed Go errors from library calls |
| Distribution | Users must install runtime separately | Single static binary (+ CGo framework links) |
| Debuggability | Logs interleaved with shell output | Structured slog events in-process |

---

## Option A: `VZBackend` — Virtualization.framework via `github.com/Code-Hex/vz`

`github.com/Code-Hex/vz` provides idiomatic Go bindings for Apple's
`Virtualization.framework` via CGo. It is the same underlying framework used by
`apple/container` and `podman machine`, but consumed as a Go library with no external
binary dependency.

### Properties

| Property | Detail |
|---|---|
| Isolation | Micro-VM (full Linux kernel per container) |
| Image format | OCI layers pulled in-process via `go-containerregistry` |
| Volume mounts | `virtio-fs` configured directly in `vz.VirtualMachineConfiguration` |
| Network | `vz.NewNATNetworkDeviceConfiguration()` (NAT only; host networking not available) |
| Resource limits | `vz.VirtualMachineConfiguration.CPUCount` / `.MemorySize` |
| Daemon | None — each VM is owned by the Wallfacer process |
| Kill | `vm.RequestStop()` → `vm.Stop()` |
| macOS requirement | macOS 13+ (Virtualization.framework, `virtio-fs` device) |
| Go build tag | `//go:build darwin` |

### Dependencies

```
github.com/Code-Hex/vz/v3                  # Virtualization.framework Go bindings (CGo)
github.com/google/go-containerregistry      # OCI image pull, layer extraction
```

### Implementation Sketch

A `VZBackend` implements `sandbox.Backend` by owning the full VM lifecycle:

```go
// internal/sandbox/vz_darwin.go
//go:build darwin

package sandbox

import (
    "context"
    "sync"

    vz "github.com/Code-Hex/vz/v3"
)

type VZBackend struct {
    imageCache string        // local directory for unpacked OCI rootfs layers
    mu         sync.Mutex
    vms        map[string]*vz.VirtualMachine // keyed by container name
}

func (b *VZBackend) Launch(ctx context.Context, spec ContainerSpec) (Handle, error) {
    // 1. Pull and cache OCI image locally (skip if digest matches).
    rootfs, err := b.ensureRootfs(spec.Image)
    if err != nil {
        return nil, err
    }

    // 2. Build vz.VirtualMachineConfiguration from ContainerSpec fields.
    cfg, err := buildVMConfig(rootfs, spec.Volumes, spec.Cmd, spec.CPUs, spec.Memory)
    if err != nil {
        return nil, err
    }

    // 3. Start VM; wire virtio-serial to stdout pipe.
    vm, stdoutPipe, err := startVM(ctx, cfg)
    if err != nil {
        return nil, err
    }
    b.mu.Lock()
    b.vms[spec.Name] = vm
    b.mu.Unlock()

    // 4. Return handle — caller reads from Stdout()/Stderr(), calls Wait()/Kill().
    return &vzHandle{
        name:   spec.Name,
        vm:     vm,
        stdout: stdoutPipe,
        backend: b,
    }, nil
}

func (b *VZBackend) List(ctx context.Context) ([]ContainerInfo, error) {
    // Return currently tracked VMs
    // ...
}
```

### VM Boot Sequence

```
┌─────────────────────────────────────────────────────┐
│ VZBackend.Launch                                   │
│                                                     │
│  1. go-containerregistry: pull OCI layers          │
│     └─ cache by image digest → skip if present     │
│                                                     │
│  2. Flatten layers into rootfs/ dir (overlay-sim)  │
│     └─ tar.Extract each layer in order             │
│                                                     │
│  3. vz.NewVirtualMachineConfiguration:             │
│     ├─ LinuxBootLoader (embedded kernel + initrd)  │
│     ├─ VirtioFileSystemDevice for rootfs/          │
│     ├─ VirtioFileSystemDevice per -v mount         │
│     ├─ VirtioNetworkDevice (NAT)                   │
│     ├─ VirtioSerialPort → stdout/stderr pipes      │
│     └─ VirtioVsock → exit-code channel             │
│                                                     │
│  4. vm.Start() → blocks until init exits           │
│                                                     │
│  5. init binary inside VM:                         │
│     ├─ chroot /rootfs                              │
│     ├─ mount /proc, /sys, /dev                     │
│     ├─ setenv from -e flags                        │
│     └─ exec Cmd; send exit code via vsock          │
└─────────────────────────────────────────────────────┘
```

### Embedded Kernel and Init

The VM needs a Linux kernel image and a tiny init binary. Both are compiled once and
embedded in the Wallfacer binary:

```go
//go:embed assets/vmlinuz assets/initramfs.cpio.gz assets/wallfacer-init
var vmAssets embed.FS
```

- **`vmlinuz`**: a stripped `x86_64` or `aarch64` kernel (≈10 MB, can be `xz`-compressed)
  with `virtio_fs`, `virtio_net`, `virtio_console`, and `vsock` compiled in.
- **`initramfs.cpio.gz`**: minimal initramfs that mounts `virtio-fs` rootfs and
  re-execs `wallfacer-init`.
- **`wallfacer-init`**: a small statically-linked Go binary that performs the chroot,
  mounts filesystems, sets environment variables, runs the container command, and
  sends the exit code over virtio-vsock before halting.

The `wallfacer-init` binary is cross-compiled for Linux (`GOOS=linux GOARCH=arm64`)
and embedded at build time via a `go generate` step.

### OCI Layer Cache

`go-containerregistry` pulls OCI images without a daemon:

```go
img, err := crane.Pull(imageRef, crane.WithInsecure())
// img.Layers() → iterate and extract to rootfs/ dir
// cache key: image digest → skip pull if already cached
```

Cache layout: `~/.wallfacer/image-cache/<digest>/rootfs/`

Layers are applied with whiteout handling (`.wh.` prefix files → delete).

### ContainerSpec Mapping

`VZBackend.Launch(ctx, spec)` receives a structured `sandbox.ContainerSpec` directly —
no CLI arg parsing needed. The mapping is straightforward:

- `spec.Name` → VM identifier for the registry
- `spec.EnvFile` → read env vars into init's environment
- `spec.Env` → set in init's environment
- `spec.Volumes` → create `VirtioFileSystemDevice` entries per `VolumeMount`
- `spec.WorkDir` → set working directory for the chroot exec
- `spec.CPUs` → `cfg.SetCPUCount(int(N))`
- `spec.Memory` → `cfg.SetMemorySize(bytes(N))`
- `spec.Network` → always NAT; `host` remapped automatically
- `spec.Image` → OCI reference to pull
- `<cmd...>` → command run inside chroot

### Trade-offs

| Pro | Con |
|---|---|
| No external binary dependency | CGo required (breaks pure Go cross-compilation) |
| Full Linux kernel isolation | macOS 13+ only |
| Context cancellation via `vm.RequestStop()` | Must bundle kernel + init binary (~15 MB) |
| OCI images pulled in-process | First pull blocks; subsequent runs hit cache |
| Single Wallfacer binary for distribution | `go-containerregistry` adds ~8 MB to binary |
| Proper error types; no exit-code guessing | Higher implementation complexity than CLI wrapper |

---

## Option B: `SandboxInitBackend` — CGo `sandbox_init(3)`

Instead of invoking the system `sandbox-exec` binary, a `SandboxInitBackend` calls
the private `sandbox_init(3)` C API directly from Go via CGo. The sandbox restriction
is applied to a forked child process before it execs Claude Code. No external binary
dependency; Claude Code must be installed on the host.

### Properties

| Property | Detail |
|---|---|
| Isolation | Syscall/path filtering via SBPL (no namespace, no VM) |
| Image format | N/A — runs host Claude Code binary |
| Volume mounts | N/A — host filesystem restricted by SBPL profile |
| Network | Allow/deny via SBPL `(allow network-outbound)` |
| Resource limits | `setrlimit(2)` in child before exec (CPU, memory, fds) |
| Daemon | None |
| macOS requirement | All versions |
| Go build tag | `//go:build darwin` |

### CGo Wrapper

```go
// internal/sandbox/cgo_darwin.go
//go:build darwin

package sandbox

/*
#cgo LDFLAGS: -framework Sandbox
#include <sandbox.h>
#include <stdlib.h>

int apply_sandbox(const char *profile) {
    char *errMsg = NULL;
    int ret = sandbox_init(profile, 0, &errMsg);
    if (ret != 0) {
        sandbox_free_error(errMsg);
    }
    return ret;
}
*/
import "C"
import (
    "fmt"
    "unsafe"
)

func applySandboxProfile(profile string) error {
    cp := C.CString(profile)
    defer C.free(unsafe.Pointer(cp))
    if C.apply_sandbox(cp) != 0 {
        return fmt.Errorf("sandbox_init failed")
    }
    return nil
}
```

### Re-exec Pattern

Go's runtime makes it unsafe to call arbitrary C code between `fork` and `exec`.
The standard Go solution is the **re-exec trick**: the Wallfacer binary detects when
it is invoked with a special env var and acts as the sandbox shim:

```go
// cmd/wallfacer-sandbox-shim/main.go (or init() block in main package)
func init() {
    if os.Getenv("_WALLFACER_SANDBOX_SHIM") == "1" {
        runSandboxShim() // apply sandbox then exec target
        os.Exit(1)       // unreachable
    }
}

func runSandboxShim() {
    profile := os.Getenv("_WALLFACER_SANDBOX_PROFILE")
    target  := os.Getenv("_WALLFACER_SANDBOX_TARGET")   // e.g. /usr/local/bin/claude
    argsRaw := os.Getenv("_WALLFACER_SANDBOX_ARGS")     // JSON-encoded []string

    if err := applySandboxProfile(profile); err != nil {
        fmt.Fprintln(os.Stderr, "sandbox_init:", err)
        os.Exit(125)
    }

    var args []string
    json.Unmarshal([]byte(argsRaw), &args)
    syscall.Exec(target, append([]string{target}, args...), os.Environ())
}
```

`SandboxInitBackend.Launch` then spawns `wallfacer` (itself) with
`_WALLFACER_SANDBOX_SHIM=1` and the encoded profile/target/args. stdout/stderr are
piped back as normal and returned via the `sandbox.Handle`.

### SBPL Profile Template

```scheme
(version 1)
(deny default)

;; System libraries (read-only)
(allow file-read* (subpath "/usr/lib"))
(allow file-read* (subpath "/usr/local/lib"))
(allow file-read* (subpath "/System"))
(allow file-read* (subpath "/private/var/db/dyld"))
(allow file-read* (subpath "/Library/Apple"))

;; Task worktree (read-write)
(allow file-read* file-write* (subpath "{{.WorktreePath}}"))

;; Claude config and instructions (read-only)
(allow file-read* (subpath "{{.ClaudeConfigDir}}"))
(allow file-read* (literal "{{.InstructionsPath}}"))

;; Temp files (claude writes here)
(allow file-read* file-write* (subpath "/tmp"))
(allow file-read* file-write* (subpath "/private/tmp"))

;; Process execution
(allow process-exec (literal "{{.ClaudeBinary}}"))
(allow process-exec (subpath "/usr/bin"))    ;; git, node, etc.
(allow process-exec (subpath "/usr/local/bin"))

;; Networking (Anthropic API)
(allow network-outbound)
(allow network-inbound (local ip "localhost:*"))

;; Mach IPC (required by runtime)
(allow mach-lookup)
(allow ipc-posix-shm)
(allow signal (target self))
```

### `SandboxInitBackend` Implementation

```go
// internal/sandbox/sandbox_init_darwin.go
//go:build darwin

package sandbox

type SandboxInitBackend struct {
    claudeBinary     string // e.g. /usr/local/bin/claude
    claudeConfigDir  string // e.g. ~/.claude
    instructionsPath string // read-only CLAUDE.md mount path
}

func (b *SandboxInitBackend) Launch(ctx context.Context, spec ContainerSpec) (Handle, error) {
    // Extract worktree path from spec.Volumes (first RW bind mount)
    worktree := findWorktreeMount(spec.Volumes)
    profile := renderSBPLProfile(sbplTemplate, sbplVars{
        WorktreePath:     worktree,
        ClaudeConfigDir:  b.claudeConfigDir,
        InstructionsPath: b.instructionsPath,
        ClaudeBinary:     b.claudeBinary,
    })

    argsJSON, _ := json.Marshal(spec.Cmd)
    cmd := exec.CommandContext(ctx, os.Executable())
    cmd.Env = append(os.Environ(),
        "_WALLFACER_SANDBOX_SHIM=1",
        "_WALLFACER_SANDBOX_PROFILE="+profile,
        "_WALLFACER_SANDBOX_TARGET="+b.claudeBinary,
        "_WALLFACER_SANDBOX_ARGS="+string(argsJSON),
    )
    // ... pipe stdout/stderr, cmd.Start(), return sandbox.Handle
}

func (b *SandboxInitBackend) List(ctx context.Context) ([]ContainerInfo, error) {
    // Return currently tracked sandbox processes
}
```

### Trade-offs

| Pro | Con |
|---|---|
| No VM overhead; native performance | No filesystem namespace; host paths visible |
| No binary to install | `sandbox_init` is a private/deprecated API |
| Works on all macOS versions | Requires Claude Code on host |
| Low implementation complexity | SBPL may need tuning per Claude Code version |
| CGo only for sandbox call | Re-exec pattern adds subprocess startup overhead |
| No image to build or cache | No CPU/memory hard limits (only `setrlimit` soft limits) |

---

## Option C: External CLI Fallback (existing approach, kept for Linux/non-macOS)

The existing `LocalBackend` remains the default on Linux and when a container
runtime is detected. The pure Go options above are additive; they do not replace the
existing path for non-macOS users.

---

## Implementation Plan

### Phase 1 — `SandboxInitBackend` (lower isolation, faster to ship)

1. Add `internal/sandbox/cgo_darwin.go` — CGo `applySandboxProfile` wrapper.
2. Add `init()` re-exec shim in `main.go` (guarded by `_WALLFACER_SANDBOX_SHIM`).
3. Add `internal/sandbox/sandbox_init_darwin.go` — `SandboxInitBackend`.
4. Wire into `WALLFACER_SANDBOX_BACKEND` switch in `NewRunner()`: value `sandbox`.
5. Add `ClaudeBinary` detection (probe `which claude`, `~/.claude/local/claude`).

### Phase 2 — `VZBackend` (full isolation)

1. Add Go module dependencies: `github.com/Code-Hex/vz/v3`, `go-containerregistry`.
2. Add `cmd/wallfacer-init/` — Linux init binary (cross-compiled, embedded).
3. Add kernel + initramfs build tooling under `build/vmlinuz/` with `go generate`.
4. Implement `internal/sandbox/vz_darwin.go` — `VZBackend`.
5. Implement OCI layer cache in `internal/sandbox/imagecache/`.
6. Wire into `WALLFACER_SANDBOX_BACKEND` switch: value `vz`.
7. Add `//go:build darwin` guards throughout; Linux path unchanged.

### Backend Selection

Backend is selected via `WALLFACER_SANDBOX_BACKEND` env var (parsed in `internal/envconfig/`, selected in `NewRunner()` switch). Current values: `local` (default). New values added by this spec:

```
WALLFACER_SANDBOX_BACKEND=vz        # VZBackend (Phase 2, macOS 13+ only)
WALLFACER_SANDBOX_BACKEND=sandbox   # SandboxInitBackend (Phase 1, macOS only)
WALLFACER_SANDBOX_BACKEND=local     # LocalBackend (default, any OS with podman/docker)
```

Auto-detection (when `WALLFACER_SANDBOX_BACKEND` is unset or `local`):

```
macOS 13+  → probe VZBackend availability → fall through to LocalBackend
macOS <13  → LocalBackend (requires podman/docker) or SandboxInitBackend
Linux      → LocalBackend only
```

### Network Mode Mapping

| Requested | VZBackend | SandboxInitBackend | LocalBackend |
|---|---|---|---|
| `host` (default) | NAT (remapped automatically) | N/A (shares host) | `--network=host` |
| `nat` | NAT | N/A | `--network=bridge` |
| `none` | no NIC | SBPL `(deny network*)` | `--network=none` |

---

## Summary Comparison

| | `VZBackend` | `SandboxInitBackend` | `LocalBackend` |
|---|---|---|---|
| External binary | None | None | podman / docker |
| Isolation level | Full Linux kernel | Syscall/path filter | Full Linux kernel |
| OCI image support | Yes (in-process pull) | No (host binaries) | Yes |
| macOS requirement | 13+ | All versions | All (with runtime) |
| Boot overhead | ~300 ms (first), ~100 ms (warm) | ~10 ms | ~200–500 ms |
| Implementation size | ~800 LOC + build tooling | ~200 LOC | Existing (0 new LOC) |
| Go build | CGo (darwin only) | CGo (darwin only) | stdlib only |
| Recommended use | Production / multi-user | Local dev / zero-install | Linux / existing users |
