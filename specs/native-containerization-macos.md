# Native Containerization: macOS

**Date:** 2026-03-13

## Problem

Wallfacer currently requires Docker or Podman to execute tasks. On macOS, both carry
significant friction: Docker Desktop requires a commercial licence for teams, and
Podman requires a separate VM daemon (`podman machine`). A native alternative would
lower the barrier to entry and remove the daemon dependency entirely.

The original spec explored external CLI tools (`apple/container`, `vfkit`, `sandbox-exec`,
Lima). This revised spec focuses on **pure Go implementations** — i.e., replacing the
`osContainerExecutor` (which shells out to an external binary) with Go code that drives
macOS platform APIs directly. No external container runtime binary is required at
runtime; the isolation logic is compiled into the Wallfacer binary.

## Scope

This spec covers macOS-only isolation techniques that can replace the
`osContainerExecutor` without changing the rest of the runner architecture.
The `ContainerExecutor` interface (`RunArgs` / `Kill`) remains the contract; only
the implementation changes.

```go
type ContainerExecutor interface {
    RunArgs(ctx context.Context, name string, args []string) (stdout, stderr []byte, err error)
    Kill(name string)
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

## Option A: `VZExecutor` — Virtualization.framework via `github.com/Code-Hex/vz`

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

A `VZExecutor` satisfies `ContainerExecutor` by owning the full VM lifecycle:

```go
// internal/runner/executor_vz_darwin.go
//go:build darwin

package runner

import (
    "context"
    "sync"

    vz "github.com/Code-Hex/vz/v3"
    "github.com/google/go-containerregistry/pkg/crane"
)

type VZExecutor struct {
    imageCache string        // local directory for unpacked OCI rootfs layers
    mu         sync.Mutex
    vms        map[string]*vz.VirtualMachine // keyed by container name
}

func (e *VZExecutor) RunArgs(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
    // 1. Parse image name from args (last non-flag token before Cmd).
    image, mounts, cmd := parseContainerArgs(args)

    // 2. Pull and cache OCI image locally (skip if digest matches).
    rootfs, err := e.ensureRootfs(image)
    if err != nil {
        return nil, nil, err
    }

    // 3. Build vz.VirtualMachineConfiguration.
    cfg, err := buildVMConfig(rootfs, mounts, cmd)
    if err != nil {
        return nil, nil, err
    }

    // 4. Start VM; wire virtio-serial to stdout/stderr buffers.
    vm, stdout, stderr, err := startVM(ctx, cfg)
    if err != nil {
        return nil, nil, err
    }
    e.mu.Lock()
    e.vms[name] = vm
    e.mu.Unlock()

    // 5. Wait for VM to stop; collect exit code via virtio-vsock.
    exitCode, err := waitVM(ctx, vm, stdout, stderr)
    e.mu.Lock()
    delete(e.vms, name)
    e.mu.Unlock()

    if exitCode != 0 {
        return stdout.Bytes(), stderr.Bytes(), &exitError{code: exitCode}
    }
    return stdout.Bytes(), stderr.Bytes(), err
}

func (e *VZExecutor) Kill(name string) {
    e.mu.Lock()
    vm := e.vms[name]
    e.mu.Unlock()
    if vm != nil {
        vm.RequestStop()  //nolint:errcheck
        vm.Stop()         //nolint:errcheck
    }
}
```

### VM Boot Sequence

```
┌─────────────────────────────────────────────────────┐
│ VZExecutor.RunArgs                                  │
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

### ContainerSpec Compatibility

`ContainerSpec.Build()` produces flags in the existing format. `VZExecutor.RunArgs`
receives the same `args []string` as `osContainerExecutor` and must parse:

- `--name <n>` → ignored (name is passed separately)
- `--env-file <f>` → read env vars into init's environment
- `-e K=V` → set in init's environment
- `-v HOST:CT[:opts]` → create additional `VirtioFileSystemDevice` entries
- `-w WORKDIR` → set working directory for the chroot exec
- `--cpus N` → `cfg.SetCPUCount(int(N))`
- `--memory N` → `cfg.SetMemorySize(bytes(N))`
- `--network=...` → always NAT; `host` remapped automatically
- `<image>` → OCI reference to pull
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

## Option B: `SandboxInitExecutor` — CGo `sandbox_init(3)`

Instead of invoking the system `sandbox-exec` binary, a `SandboxInitExecutor` calls
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
// internal/runner/sandbox_darwin.go
//go:build darwin

package runner

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

`SandboxInitExecutor.RunArgs` then spawns `wallfacer` (itself) with
`_WALLFACER_SANDBOX_SHIM=1` and the encoded profile/target/args. stdout/stderr are
piped back as normal.

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

### `SandboxInitExecutor` Implementation

```go
// internal/runner/executor_sandbox_darwin.go
//go:build darwin

package runner

type SandboxInitExecutor struct {
    claudeBinary     string // e.g. /usr/local/bin/claude
    claudeConfigDir  string // e.g. ~/.claude
    instructionsPath string // read-only CLAUDE.md mount path
}

func (e *SandboxInitExecutor) RunArgs(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
    worktree, claudeArgs := parseWorktreeAndArgs(args)
    profile := renderSBPLProfile(sbplTemplate, sbplVars{
        WorktreePath:     worktree,
        ClaudeConfigDir:  e.claudeConfigDir,
        InstructionsPath: e.instructionsPath,
        ClaudeBinary:     e.claudeBinary,
    })

    argsJSON, _ := json.Marshal(claudeArgs)
    cmd := exec.CommandContext(ctx, os.Executable())
    cmd.Env = append(os.Environ(),
        "_WALLFACER_SANDBOX_SHIM=1",
        "_WALLFACER_SANDBOX_PROFILE="+profile,
        "_WALLFACER_SANDBOX_TARGET="+e.claudeBinary,
        "_WALLFACER_SANDBOX_ARGS="+string(argsJSON),
    )
    // ... pipe stdout/stderr, run, return
}

func (e *SandboxInitExecutor) Kill(name string) {
    // name → pid stored in a sync.Map; send SIGKILL
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

The existing `osContainerExecutor` remains the default on Linux and when a container
runtime is detected. The pure Go options above are additive; they do not replace the
existing path for non-macOS users.

---

## Implementation Plan

### Phase 1 — `SandboxInitExecutor` (lower isolation, faster to ship)

1. Add `internal/runner/sandbox_darwin.go` — CGo `applySandboxProfile` wrapper.
2. Add `init()` re-exec shim in `main.go` (guarded by `_WALLFACER_SANDBOX_SHIM`).
3. Add `internal/runner/executor_sandbox_darwin.go` — `SandboxInitExecutor`.
4. Wire into runtime detection: when `CONTAINER_CMD=sandbox` or no container runtime
   found on macOS, use `SandboxInitExecutor`.
5. Add `ClaudeBinary` detection (probe `which claude`, `~/.claude/local/claude`).

### Phase 2 — `VZExecutor` (full isolation)

1. Add Go module dependencies: `github.com/Code-Hex/vz/v3`, `go-containerregistry`.
2. Add `cmd/wallfacer-init/` — Linux init binary (cross-compiled, embedded).
3. Add kernel + initramfs build tooling under `build/vmlinuz/` with `go generate`.
4. Implement `internal/runner/executor_vz_darwin.go` — `VZExecutor`.
5. Implement OCI layer cache in `internal/runner/imagecache/`.
6. Wire into runtime detection above `sandbox` in the priority list.
7. Add `//go:build darwin` guards throughout; Linux path unchanged.

### Runtime Detection Priority (Revised)

```
CONTAINER_CMD env var                         # explicit override always wins
→ vz          (VZExecutor, darwin only)       # Phase 2 pure Go VM
→ sandbox     (SandboxInitExecutor, darwin)   # Phase 1 pure Go sandbox
→ container   (/usr/local/bin/container)      # apple/container CLI (existing)
→ /opt/podman/bin/podman                      # existing
→ podman                                      # existing
→ docker                                      # existing
```

Auto-detection on macOS (when `CONTAINER_CMD` is unset):

```
macOS 13+  → probe VZExecutor availability → fall through to CLI runtimes → sandbox
macOS <13  → probe CLI runtimes → sandbox
Linux      → probe CLI runtimes only
```

### Network Mode Mapping

| Requested | VZExecutor | SandboxInitExecutor | osContainerExecutor |
|---|---|---|---|
| `host` (default) | NAT (remapped automatically) | N/A (shares host) | `--network=host` |
| `nat` | NAT | N/A | `--network=bridge` |
| `none` | no NIC | SBPL `(deny network*)` | `--network=none` |

---

## Summary Comparison

| | `VZExecutor` | `SandboxInitExecutor` | `osContainerExecutor` |
|---|---|---|---|
| External binary | None | None | podman / docker |
| Isolation level | Full Linux kernel | Syscall/path filter | Full Linux kernel |
| OCI image support | Yes (in-process pull) | No (host binaries) | Yes |
| macOS requirement | 13+ | All versions | All (with runtime) |
| Boot overhead | ~300 ms (first), ~100 ms (warm) | ~10 ms | ~200–500 ms |
| Implementation size | ~800 LOC + build tooling | ~200 LOC | Existing (0 new LOC) |
| Go build | CGo (darwin only) | CGo (darwin only) | stdlib only |
| Recommended use | Production / multi-user | Local dev / zero-install | Linux / existing users |
