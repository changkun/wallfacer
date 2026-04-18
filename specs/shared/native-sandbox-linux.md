---
title: "Native Containerization: Linux"
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


# Native Containerization: Linux

## Problem

Wallfacer currently requires Docker or Podman as a runtime dependency. On Linux,
Docker requires a system daemon (`dockerd`) running as root, and Podman — while
daemonless — still requires installation. Several lightweight, daemon-free
alternatives exist that are either already present on most Linux distributions or
are trivial to install, covering a range of isolation depths.

## Scope

This spec covers Linux-native alternatives to Docker/Podman that can back the
`SandboxBackend` interface (see [sandbox-backends.md](../foundations/sandbox-backends.md)).
The goal is to support at least one option that ships with a stock Linux
installation with no extra packages.

**Prerequisite:** [sandbox-backends.md](../foundations/sandbox-backends.md) Phase 1 must
be complete. Each native executor is implemented as a `SandboxBackend` (not the
retired `ContainerExecutor`), receiving a structured `ContainerSpec` via
`Launch(ctx, spec)` instead of parsing CLI args. The returned `SandboxHandle`
tracks lifecycle states (`Creating` → `Running` → `Streaming` → `Stopped`/`Failed`).

---

## Option A: `bubblewrap` (`bwrap`) — Recommended

[bubblewrap](https://github.com/containers/bubblewrap) is a low-privilege sandboxing
tool developed by the Flatpak project. It uses Linux user namespaces and bind mounts
to create an isolated environment without root or a daemon.

### Properties

| Property | Detail |
|---|---|
| Isolation | User/pid/mount/network namespaces |
| Image format | N/A — uses host or pre-extracted rootfs directories |
| Volume mounts | `--bind HOST CONTAINER` / `--ro-bind` |
| Network | Unshared network namespace by default; can share host network with `--share-net` |
| Resource limits | Via `--die-with-parent`; CPU/memory via cgroup v2 (`systemd-run --scope`) |
| Daemon | None |
| Install | Ships with most distros (Fedora, Debian, Ubuntu, Arch) |
| Root required | No — uses unprivileged user namespaces |

### Integration Plan

Implement a `BubblewrapBackend` (implementing `SandboxBackend`) that maps
`ContainerSpec` fields to `bwrap` flags:

| ContainerSpec concept | bwrap equivalent |
|---|---|
| `-v HOST:CT` | `--bind HOST CT` |
| `-v HOST:CT:ro` | `--ro-bind HOST CT` |
| `-e K=V` | `--setenv K V` |
| `-w DIR` | `--chdir DIR` |
| `--network=host` | `--share-net` |
| `--network=none` | (default — no `--share-net`) |
| `--rm` | implicit — no persistent state |
| Image | Pre-extracted OCI rootfs directory |

**OCI image → rootfs extraction** (one-time, during `make build`):
```bash
# Extract the wallfacer image to a rootfs directory
container_id=$(docker create wallfacer)
docker export $container_id | tar -C /var/lib/wallfacer/rootfs -xf -
docker rm $container_id
```

Alternatively, use `umoci` or `skopeo` + `umoci` to extract OCI images without a
Docker daemon.

**Example invocation:**
```bash
bwrap \
  --ro-bind /var/lib/wallfacer/rootfs / \
  --tmpfs /tmp \
  --proc /proc \
  --dev /dev \
  --bind /workspace/task-worktree /workspace/myrepo \
  --ro-bind /home/user/.wallfacer/instructions/abc.md /workspace/CLAUDE.md \
  --setenv ANTHROPIC_API_KEY $KEY \
  --chdir /workspace/myrepo \
  --share-net \
  /usr/local/bin/claude -p "..." --output-format stream-json
```

**Kill:** `bwrap` launches the process directly; store the PID and send `SIGKILL`.

### Trade-offs

| Pro | Con |
|---|---|
| Ships with most distros; often already installed | Requires pre-extracted rootfs (not OCI pull) |
| No daemon, no root | Rootfs extraction step in build pipeline |
| Fast — near-native process startup | No built-in CPU/memory limits (need cgroup wrap) |
| Widely used (Flatpak, Chromium, Firefox) | `--share-net` needed for Anthropic API |

---

## Option B: `systemd-nspawn`

`systemd-nspawn` is a container manager built into systemd, available on virtually
every modern Linux distribution that uses systemd (Fedora, Debian, Ubuntu, Arch, etc.).

### Properties

| Property | Detail |
|---|---|
| Isolation | Full OS namespace container (pid, mount, uts, ipc, net) |
| Image format | Directory trees or disk images |
| Volume mounts | `--bind=HOST:CT` / `--bind-ro=HOST:CT` |
| Network | Virtual Ethernet by default; `--network-veth` or `--network-zone` |
| Resource limits | Full cgroup v2 integration (`--property=CPUQuota=200%`) |
| Daemon | None for one-shot (`systemd-nspawn --ephemeral`); optional `machinectl` |
| Install | Ships with systemd — zero extra packages on most distros |
| Root required | Yes for full isolation; or use `--user` namespace with some limits |

### Integration Plan

Implement a `NspawnBackend` (implementing `SandboxBackend`):

```bash
systemd-nspawn \
  --ephemeral \
  --directory=/var/lib/wallfacer/rootfs \
  --bind=/workspace/task-worktree:/workspace/myrepo \
  --bind-ro=/home/user/.wallfacer/instructions/abc.md:/workspace/CLAUDE.md \
  --setenv=ANTHROPIC_API_KEY=$KEY \
  --network-veth \
  --chdir=/workspace/myrepo \
  --property=CPUQuota=200% \
  --property=MemoryMax=4G \
  /usr/local/bin/claude -p "..." --output-format stream-json
```

`--ephemeral` creates a throwaway overlay snapshot of the rootfs on each run,
equivalent to `--rm`.

**Kill:** `machinectl terminate <name>` or `kill -9 <nspawn-pid>`.

### Trade-offs

| Pro | Con |
|---|---|
| Zero install on systemd distros | Needs root (or sudo policy) for full isolation |
| Full cgroup resource limits built in | Network setup more complex than `--network=host` |
| `--ephemeral` gives clean per-run state | Not available on non-systemd distros (Alpine, Gentoo) |
| Excellent for CI/CD server environments | Heavier than bubblewrap |

---

## Option C: `unshare` (util-linux)

`unshare` creates new Linux namespaces for a process. It is part of `util-linux`,
which is present on every Linux distribution.

### Properties

| Property | Detail |
|---|---|
| Isolation | User/pid/mount/net/uts namespaces (selectable) |
| Image format | N/A — uses host filesystem + bind mounts via `mount --bind` |
| Volume mounts | Requires separate `mount --bind` calls before exec |
| Network | `--net` unshares network; `--map-root-user` enables rootless |
| Daemon | None |
| Install | `util-linux` — present on all distros |
| Root required | No with `--map-root-user` (user namespaces must be enabled) |

### Integration Plan

`unshare` provides the namespace primitives but not volume mount management.
A thin shell script or Go code would need to:

1. Call `unshare --mount --pid --net --fork --map-root-user`
2. Inside the new namespace, `mount --bind` each workspace directory
3. `exec` the Claude Code binary

This is more assembly work than Option A/B but relies on universally available
tools.

### Trade-offs

| Pro | Con |
|---|---|
| Present on every Linux install | No integrated volume mount management |
| Maximum control over which namespaces to use | Requires custom mount orchestration code |
| Useful as a building block | More fragile than purpose-built tools |

---

## Option D: `firejail`

[firejail](https://firejail.wordpress.com/) is a SUID sandbox that uses Linux
namespaces and seccomp-bpf. Commonly pre-installed on Ubuntu and Debian.

### Properties

| Property | Detail |
|---|---|
| Isolation | User/pid/mount/net namespaces + seccomp |
| Volume mounts | `--whitelist=PATH` or `--bind-try=HOST:CT` |
| Network | `--net=none`, `--net=br0`, or default (host shared) |
| Daemon | None |
| Install | Available in most distros (`apt install firejail`) |
| Root required | No (SUID binary) |

### Integration Plan

```bash
firejail \
  --noprofile \
  --private=/var/lib/wallfacer/rootfs \
  --bind=/workspace/task-worktree,/workspace/myrepo \
  --env=ANTHROPIC_API_KEY=$KEY \
  --net=br0 \
  /usr/local/bin/claude -p "..." --output-format stream-json
```

**Kill:** `firejail --shutdown=<pid>` or `kill -9 <pid>`.

### Trade-offs

| Pro | Con |
|---|---|
| Good seccomp-bpf filtering | SUID binary (security trade-off) |
| No daemon | Less suitable for server/CI environments |
| Available on Ubuntu/Debian by default | Less maintained than bubblewrap |

---

## Option E: `nsjail` (Google)

[nsjail](https://github.com/google/nsjail) is a security-oriented namespace jail
from Google, widely used in CTF hosting and cloud sandboxes.

### Properties

| Property | Detail |
|---|---|
| Isolation | All Linux namespaces + seccomp + rlimits |
| Volume mounts | Config file (`--bindmount HOST:CT`) or CLI flags |
| Network | `--disable_clone_newnet` to share host; default is isolated |
| Resource limits | Native rlimits and cgroup v2 |
| Daemon | None |
| Install | Build from source or package (not pre-installed) |
| Root required | No (user namespaces) |

Best suited for high-security deployments where the task code must be treated as
fully untrusted. Requires installation; not a zero-dependency option.

---

## Recommended Implementation Order

1. **`bubblewrap`** — best balance of isolation, availability, and implementation
   cost. Most distros ship it; zero-install on Fedora/Ubuntu/Arch. Implement
   `BubblewrapBackend` (implementing `SandboxBackend`).
2. **`systemd-nspawn`** — add as a `NspawnBackend` for server/CI deployments where
   root is available and cgroup resource limits are required.
3. **`unshare`** — document as a building block; implement only if bubblewrap is
   unavailable.
4. **`firejail` / `nsjail`** — document as `CONTAINER_CMD` overrides or manual
   wrapper scripts; no first-class Wallfacer code required.

## Runtime Detection Priority (Proposed)

```
CONTAINER_CMD env var                          # explicit override always wins
→ /opt/podman/bin/podman
→ podman
→ docker
→ bwrap                                        # bubblewrap — zero-install on most distros
→ systemd-nspawn                               # systemd distros with root
```

## Rootfs Management

Both `bwrap` and `systemd-nspawn` need a pre-extracted rootfs rather than an OCI
image reference. The extraction should be part of `make build`:

```makefile
rootfs: image
    mkdir -p $(ROOTFS_DIR)
    $(CONTAINER_CMD) create --name wallfacer-extract wallfacer
    $(CONTAINER_CMD) export wallfacer-extract | tar -C $(ROOTFS_DIR) -xf -
    $(CONTAINER_CMD) rm wallfacer-extract
```

Or via `skopeo` + `umoci` for fully daemonless extraction:
```bash
skopeo copy docker://wallfacer oci:wallfacer-oci:latest
umoci unpack --image wallfacer-oci:latest wallfacer-rootfs
```

When neither Docker nor Podman is available, provide a pre-built rootfs tarball as
a release asset that users can download and extract manually.

## Network Mode Mapping (Proposed)

| Current value | bubblewrap | systemd-nspawn |
|---|---|---|
| `host` (default) | `--share-net` | `--network-veth` + host bridge or `--network-host` |
| `none` | (default — no `--share-net`) | `--network-namespace=/proc/1/ns/net` isolated |
| `slirp4netns` | run under `slirp4netns bwrap ...` wrapper | N/A |
