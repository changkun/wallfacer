# Native Containerization: Windows

**Date:** 2026-03-13

## Problem

Wallfacer currently requires Docker or Podman, neither of which has a seamless
Windows-native story: Docker Desktop requires a licence for commercial use and runs
containers inside a WSL2 or Hyper-V Linux VM, while Podman for Windows is early-stage.

This spec explores Windows-native isolation alternatives and the architectural changes
needed to support them.

## Additional Prerequisite: Go Server on Windows

Before containerization is relevant, the Go server itself must build and run on
Windows. The server uses `os/exec` with Linux-centric paths (`/opt/podman/bin/podman`)
and `os/exec` signals (`SIGKILL`). A Windows port requires:

- Replace `syscall.SIGKILL` with `cmd.Process.Kill()` (already cross-platform in Go)
- Make runtime detection probe Windows-native paths (`%ProgramFiles%\...`)
- Replace `/tmp`-style tempfile paths with `os.TempDir()`
- Replace `z` SELinux volume mount options (ignored on Windows runtimes)

These are prerequisite changes regardless of which isolation backend is chosen.

---

## Option A: WSL2 + Existing Linux Runtime (Lowest Effort)

WSL2 (Windows Subsystem for Linux 2) is built into Windows 10 2004+ and Windows 11.
It runs a real Linux kernel in a Hyper-V micro-VM. Docker Desktop and Podman already
use it as their Linux layer.

### Approach

Run the **entire Wallfacer server inside WSL2**, not on the Windows host. The user
installs WSL2, clones the repo into the WSL2 filesystem, and runs `wallfacer` there.
This is not a native Windows approach but it is the **lowest-friction path to
Windows support** because nothing in the Go server or container logic changes.

### Properties

| Property | Detail |
|---|---|
| Isolation | Linux VM (Hyper-V) per WSL2 instance |
| Container runtime | Any Linux runtime inside WSL2 (Podman, bubblewrap, etc.) |
| Host integration | `localhost` port forwarding; `\\wsl$\` filesystem access |
| Browser launch | `cmd.exe /c start http://localhost:8080` from WSL2 |
| Install | WSL2 + a Linux distro (one command: `wsl --install`) |

### Integration Plan

- Detect WSL2 environment: `os.Getenv("WSL_DISTRO_NAME") != ""`
- When running inside WSL2, treat the environment as Linux and use Linux backends
- For browser launch, replace `xdg-open` with `cmd.exe /c start <url>`
- Document "Windows users: install WSL2 and run Wallfacer inside it" as the
  supported path

### Trade-offs

| Pro | Con |
|---|---|
| Zero new code for container logic | Not truly native Windows |
| All Linux runtimes work unchanged | Filesystem performance slower on WSL2 ↔ Windows mounts |
| Lowest engineering investment | Users must learn WSL2 |

---

## Option B: Windows Sandbox (AppContainer / `wdaemon`)

Windows 10/11 Pro and Enterprise include **Windows Sandbox**, a lightweight VM with
an ephemeral Windows desktop environment. It is designed for running untrusted
applications, not for running Linux workloads.

### Why It Does Not Apply

Claude Code is a Linux/macOS CLI tool and has no native Windows binary. Windows
Sandbox cannot run Linux executables without WSL2, which brings the same VM
dependency as Option A. This option is **not applicable** for running Claude Code.

---

## Option C: Hyper-V Isolated VMs (Server / Enterprise)

On Windows Server and Windows 11 Enterprise, Hyper-V provides hardware-isolated VMs.
This is the backend Docker Desktop uses for Hyper-V mode (as opposed to WSL2 mode).

### Approach

Use the Hyper-V REST API or `hvc.exe` / `vmcompute.dll` to create ephemeral Linux
VMs directly from Wallfacer, bypassing Docker/Podman.

### Properties

| Property | Detail |
|---|---|
| Isolation | Full hardware VM (VT-x) |
| Image format | VHDX disk images (converted from OCI via `wsl --import`) |
| Volume mounts | Mapped drives or `virtio-fs` shares via `hvsocket` |
| Network | Virtual switch; NAT or bridged |
| Daemon | Hyper-V service (always running on Enterprise SKUs) |
| Install | Windows 11 Pro/Enterprise; `Enable-WindowsOptionalFeature -FeatureName Microsoft-Hyper-V` |

### Integration Plan

Implement a `HyperVExecutor` that calls `hvc.exe` (or the lower-level `vmcompute`
COM API via CGo) to:

1. Create an ephemeral VM from a base VHDX (built from the wallfacer OCI image via
   `wsl --import`).
2. Mount workspace directories via `hvsocket` or a Samba/virtio-fs share.
3. Run Claude Code inside the VM via a serial console or SSH listener.
4. Terminate and delete the VM on completion.

This is the Windows equivalent of `vfkit` on macOS — a high-control, low-level
approach with significant implementation cost.

### Trade-offs

| Pro | Con |
|---|---|
| Strong hardware isolation | Windows Pro/Enterprise only (not Home) |
| Full cgroup-equivalent resource limits via VM config | Very high implementation cost |
| No third-party daemon | Complex volume sharing |
| Suitable for enterprise/server deployments | Requires VHDX image management |

---

## Option D: Job Objects + Process Isolation (Lightweight)

Windows **Job Objects** are the native mechanism for grouping processes and applying
resource limits (`CPU rate`, `memory commit`, `I/O bandwidth`). Combined with
**restricted tokens** and **AppContainer** low-integrity levels, they provide
lightweight process-level isolation without a VM.

This is analogous to `sandbox-exec` on macOS — it restricts what a process can do
but does not provide filesystem namespacing or a separate kernel.

### Properties

| Property | Detail |
|---|---|
| Isolation | Process group + restricted token (no namespace, no separate kernel) |
| Filesystem | Access control via DACL on workspace directories; no bind mounts |
| Network | Win32 firewall rules per process SID |
| Resource limits | `SetInformationJobObject` for CPU/memory limits |
| Daemon | None |
| Install | Zero — Win32 API built into every Windows version |
| Root required | No (user-mode; elevated only for firewall rules) |

### Integration Plan

Implement a `JobObjectExecutor` in Go using `golang.org/x/sys/windows`:

```go
// Pseudocode
job := windows.CreateJobObject(nil, name)
windows.SetInformationJobObject(job, JobObjectCpuRateControlInformation, &cpuRate)
windows.SetInformationJobObject(job, JobObjectExtendedLimitInformation, &memLimit)

cmd := exec.Command(claudePath, args...)
// Assign child process to job after creation
windows.AssignProcessToJobObject(job, cmd.Process.Handle)
```

**Filesystem isolation** is approximated by:
- Creating a temporary user account with write access only to the task worktree
- Running Claude Code as that user (`CreateProcessWithLogonW`)
- Revoking access after task completion

This approach does not require Claude Code to be containerized — it runs the host
Claude Code binary directly. Requires Claude Code for Windows to exist.

### Trade-offs

| Pro | Con |
|---|---|
| Zero install; built into every Windows version | No filesystem namespace — host paths visible |
| Native resource limits | Requires Claude Code native Windows binary |
| Near-native process performance | Filesystem isolation via DACLs is complex to get right |
| No VM overhead | Less isolation depth than VM-based options |

---

## Option E: `containerd` + `hcsshim` (Windows Containers)

Microsoft's `hcsshim` and `containerd` support Windows Containers — OCI containers
running Windows binaries natively. However, these cannot run Linux container images
without a Linux VM underneath.

**For running the existing `wallfacer` Linux OCI image**, this requires the LCOW
(Linux Containers on Windows) mode, which uses a Hyper-V Linux VM internally.
This is essentially Docker Desktop without the Docker UI, and carries the same
daemon dependency.

**Not recommended** as a standalone alternative; only relevant if a native Windows
Claude Code image is created.

---

## Recommended Implementation Order

| Priority | Option | Reason |
|---|---|---|
| 1 | **WSL2 (Option A)** | Zero container-code changes; ship Windows support quickly |
| 2 | **Job Objects (Option D)** | True Windows-native; prerequisite is Claude Code for Windows binary |
| 3 | **Hyper-V (Option C)** | Strong isolation for enterprise; high cost, defer |
| 4 | **Windows Sandbox / containerd** | Not applicable without native Windows Claude Code image |

## Prerequisites Before Any Windows Container Work

Before implementing any isolation backend, complete these server-level changes:

| Item | File | Change |
|---|---|---|
| Runtime detection | `main.go` | Add Windows paths; skip `/opt/podman/bin/podman` on Windows |
| Signal handling | `executor.go` | Replace `SIGKILL` with `cmd.Process.Kill()` |
| Temp paths | `runner.go`, `container.go` | Use `os.TempDir()` instead of `/tmp` |
| Browser launch | `main.go` | Use `cmd.exe /c start <url>` on Windows |
| SELinux mount opts | `container_spec.go` | Skip `,z` option on non-Linux platforms |
| Path separators | `container.go` | Use `filepath.Join` consistently |

## WSL2 Detection and Fallthrough

```go
func isWSL() bool {
    return os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != ""
}

// In browser launch:
func openBrowser(url string) {
    switch runtime.GOOS {
    case "windows":
        exec.Command("cmd", "/c", "start", url).Start()
    case "darwin":
        exec.Command("open", url).Start()
    default:
        if isWSL() {
            exec.Command("cmd.exe", "/c", "start", url).Start()
        } else {
            exec.Command("xdg-open", url).Start()
        }
    }
}
```
