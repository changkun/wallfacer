# Native Containerization: macOS

**Date:** 2026-03-13

## Problem

Wallfacer currently requires Docker or Podman to execute tasks. On macOS, both carry
significant friction: Docker Desktop requires a commercial licence for teams, and
Podman requires a separate VM daemon (`podman machine`). A native alternative would
lower the barrier to entry and remove the daemon dependency entirely.

## Scope

This spec covers macOS-only isolation techniques that can replace the
`osContainerExecutor` without changing the rest of the runner architecture.
The `ContainerExecutor` interface (`RunArgs` / `Kill`) remains the contract; only
the implementation changes.

---

## Option A: `apple/container` (Recommended)

Apple open-sourced a container CLI in 2025 at
[github.com/apple/container](https://github.com/apple/container). It is OCI-compatible
and uses Apple's `Virtualization.framework` under the hood, creating one micro-VM
per container.

### Properties

| Property | Detail |
|---|---|
| Isolation | Micro-VM (full Linux kernel per container) |
| Image format | OCI (pulls from registries) |
| Volume mounts | `virtio-fs` shares; `-v host:container` flag works |
| Network | `vmnet` or NAT; `--network host` is **not** supported |
| Resource limits | `--cpus`, `--memory` supported |
| Daemon | None — each container is an independent process |
| Kill | `container stop <name>` / `container rm -f <name>` |
| macOS requirement | macOS 15+ (Virtualization.framework) |
| Install | `brew install apple/container/container` |

### CLI Compatibility with Current ContainerSpec

The current `container_spec.go` produces flags like:

```
run --rm --network=host --name <name> --env-file <f> -e K=V -v HOST:CT -w /workspace <image> <cmd...>
```

`apple/container` accepts identical flags except `--network=host` (use `--network=nat`
or omit for the default NAT). The migration is a one-line change in network mode
resolution.

### Integration Plan

1. **Runtime detection** — probe `container` on `$PATH` and add it to the priority
   list before `podman`:
   ```
   CONTAINER_CMD env var
   → /usr/local/bin/container  (apple/container)
   → /opt/podman/bin/podman
   → podman
   → docker
   ```
2. **Network mode** — when the detected runtime is `container`, remap `"host"` →
   `"nat"` automatically (or make `WALLFACER_CONTAINER_NETWORK` default to `"nat"`
   on macOS).
3. **Kill** — `container stop <name>` then `container rm -f <name>`; same pattern as
   today.
4. **No image rebuild** — the existing `wallfacer` OCI image runs unchanged.

### Trade-offs

| Pro | Con |
|---|---|
| OCI-compatible; existing image works | `--network=host` unavailable |
| No daemon process | macOS 15+ only |
| Apple-maintained, long-term viability | VM boot adds ~200 ms per task |
| Each container = isolated kernel | Installation still required |

---

## Option B: `vfkit` (Low-level Virtualization.framework)

`vfkit` ([github.com/crc-org/vfkit](https://github.com/crc-org/vfkit)) is a
minimal CLI wrapper around `Virtualization.framework` from the Red Hat CRC team.
It boots Linux VMs in under a second.

### Properties

| Property | Detail |
|---|---|
| Isolation | Full Linux VM per invocation |
| Image format | Raw/qcow2 disk images; **no OCI layer** |
| Volume mounts | `virtio-fs` shares configured as CLI flags |
| Network | `--device virtio-net` with `--network nat` |
| Daemon | None |
| macOS requirement | macOS 12+ |

### Integration Plan

Because `vfkit` has no OCI concept, a `VfkitExecutor` would need to:

1. Maintain a base VM disk image (built from the existing container image via
   `docker export` → raw disk).
2. For each task: snapshot the disk, mount worktree dirs as `virtio-fs` shares,
   boot the VM, run Claude Code inside via a serial console or SSH, capture output,
   shut down the VM, and discard the snapshot.

This is significantly more implementation work than Option A and is better suited
to scenarios where OCI tooling is completely unavailable.

### Trade-offs

| Pro | Con |
|---|---|
| Lowest-level control | No OCI; requires custom disk image management |
| Sub-second boot | Substantial new code in runner |
| No Apple tooling dependency | Less community documentation |

---

## Option C: `sandbox-exec` (Zero-install Lightweight Mode)

macOS ships `sandbox-exec` on every installation. It runs a process under an SBPL
(Sandbox Profile Language) profile that restricts filesystem paths and network access.
No VM; the process runs natively on the host.

### Properties

| Property | Detail |
|---|---|
| Isolation | Syscall/path filtering (no namespace, no separate kernel) |
| Image format | N/A — runs host binaries |
| Volume mounts | N/A — process sees host filesystem, restricted by profile |
| Network | Allow/deny per-host or globally |
| Daemon | None |
| macOS requirement | All versions (technically deprecated since 10.10, no man page, but functional) |
| Install | None |

### Profile Skeleton

```scheme
(version 1)
(deny default)

;; Allow read-only access to system libraries
(allow file-read* (subpath "/usr/lib"))
(allow file-read* (subpath "/System"))
(allow file-read* (subpath "/private/var/db/dyld"))

;; Allow read-write access to the task worktree only
(allow file-read* file-write* (subpath "/workspace/TASK_WORKTREE_PATH"))

;; Allow read-only access to Claude config and instructions
(allow file-read* (subpath "/home/USER/.claude"))
(allow file-read* (literal "/workspace/CLAUDE.md"))

;; Allow outbound network (for Anthropic API)
(allow network-outbound)

;; Allow process execution for claude binary
(allow process-exec (literal "/usr/local/bin/claude"))
```

### Integration Plan

Implement a `SandboxExecExecutor` that:

1. Writes an SBPL profile to a temp file with the task's worktree path substituted.
2. Invokes: `sandbox-exec -f <profile> /usr/local/bin/claude <args...>`
3. Captures stdout/stderr as today.
4. Kill: `kill <pid>` — no container name needed.
5. Requires Claude Code to be installed on the host machine.

This mode trades isolation depth for zero dependencies. It is appropriate for
individual developers running Wallfacer locally on trusted workspaces.

### Trade-offs

| Pro | Con |
|---|---|
| Zero install — ships with macOS | Technically deprecated; SBPL is undocumented |
| No VM overhead; native performance | No filesystem namespace; host paths visible |
| No image to build or maintain | Requires Claude Code on host |
| Works on all macOS versions | No resource limits (CPU/memory) |

---

## Option D: Lima

[Lima](https://github.com/lima-vm/lima) manages Linux VMs on macOS and exposes a
`nerdctl` interface (containerd-compatible). It can replace Podman transparently for
users already familiar with container workflows.

### Properties

| Property | Detail |
|---|---|
| Isolation | Linux VM (QEMU or Virtualization.framework backend) |
| Image format | OCI via containerd / nerdctl |
| Volume mounts | Automatic host directory sharing |
| Network | Port forwarding; `--network host` equivalent via socket forwarding |
| Daemon | `limactl` manages long-running VMs |
| Install | `brew install lima` |

### Integration Plan

Lima is most useful as a `CONTAINER_CMD=limactl nerdctl` override rather than
a first-class Wallfacer runtime, since the existing `ContainerSpec` flags map
cleanly to `nerdctl` syntax. No code changes required; documentation update only.

---

## Recommended Implementation Order

1. **`apple/container`** — highest isolation, lowest migration cost, best long-term
   support. Requires only network-mode remapping and binary detection.
2. **`sandbox-exec`** — add as a `SandboxExecExecutor` for zero-install local dev.
   Gate behind `CONTAINER_CMD=sandbox-exec` or a `--sandbox exec` flag.
3. **Lima** — document as a `CONTAINER_CMD` override; no code required.
4. **`vfkit`** — defer; high implementation cost for marginal gain over Option A.

## Runtime Detection Priority (Proposed)

```
CONTAINER_CMD env var                          # explicit override always wins
→ container       (/usr/local/bin/container)   # apple/container
→ /opt/podman/bin/podman
→ podman
→ docker
→ sandbox-exec    (fallback, local dev only)
```

## Network Mode Mapping (Proposed)

| Current value | apple/container | sandbox-exec |
|---|---|---|
| `host` (default) | `nat` | N/A (process shares host network) |
| `none` | `none` | restrict via SBPL `(deny network*)` |
| `slirp4netns` | `nat` | N/A |
