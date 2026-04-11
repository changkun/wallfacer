---
title: K8s Sandbox Backend
status: drafted
depends_on:
  - specs/cloud/tenant-filesystem.md
  - specs/foundations/sandbox-backends.md
affects: [internal/sandbox/]
effort: xlarge
created: 2026-03-28
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# K8s Sandbox Backend

## Problem

The `LocalBackend` (`internal/sandbox/`) launches containers via `os/exec` on the host. This works for single-machine deployment but limits cloud deployment: per-user instances (multi-tenant) need to dispatch sandbox containers to a shared cluster without giving each instance a local container runtime.

The `sandbox.Backend` interface (sandbox backends) already abstracts container lifecycle. This spec implements `K8sBackend` — a backend that dispatches containers as Kubernetes Jobs.

## Design

### K8sBackend

```go
// K8sBackend implements sandbox.Backend using Kubernetes Jobs.
type K8sBackend struct {
    client       kubernetes.Interface
    namespace    string // per-tenant or shared namespace
    hotPathRoot  string // fs.latere.ai workspace hot path (from tenant filesystem)
}
```

**Mapping `ContainerSpec` → K8s Job:**

| ContainerSpec field | K8s equivalent |
|---------------------|----------------|
| `Image` | `pod.spec.containers[0].image` |
| `Env` | `pod.spec.containers[0].env` |
| `Cmd` | `pod.spec.containers[0].command` + `args` |
| `CPUs` | `resources.limits.cpu` |
| `Memory` | `resources.limits.memory` |
| `Network` | NetworkPolicy (see below) |
| `Volumes` | PVC subPath mounts (see below) |
| `WorkingDir` | `pod.spec.containers[0].workingDir` |

### Volume Mount Assembly

The tenant filesystem (tenant filesystem) provides repos and worktrees on the fs.latere.ai hot tier. The K8s backend translates `ContainerSpec.Volumes` into host path mounts pointing at the hot tier:

```go
// Local backend (current):
//   VolumeMount{Host: "/home/user/.wallfacer/worktrees/<task>/project-a", Container: "/workspace/project-a"}
//
// K8s backend (via fs.latere.ai hot path):
//   VolumeMount{Host: "/hot/<workspace-id>/worktrees/<task>/project-a", Container: "/workspace/project-a"}
```

The backend needs to know the fs.latere.ai workspace hot path root to translate host-path prefixes. This comes from `K8sBackend.hotPathRoot`, set at instance startup when the fs.latere.ai workspace is created.

Other mounts (instructions file, board context, sibling worktrees) follow the same pattern — all are subdirectories of the hot tier workspace.

Since the hot tier is local disk on the compute node, both the wallfacer server pod and sandbox pods must be scheduled on the same node. This is the same node-affinity constraint as the original PVC design, but managed by fs.latere.ai rather than a standalone PVC.

### Handle Implementation

```go
type k8sHandle struct {
    client    kubernetes.Interface
    namespace string
    jobName   string
    podName   string   // resolved after pod scheduling
}
```

| sandbox.Handle method | K8s implementation |
|---|---|
| `Wait(ctx)` | Watch pod phase until Succeeded/Failed |
| `Stop(ctx)` | Delete the Job (propagation: Foreground) |
| `Logs(ctx)` | `client.CoreV1().Pods().GetLogs()` with follow |
| `Exec(ctx, cmd)` | `remotecommand.NewSPDYExecutor` into running pod |
| `State()` | Map pod phase → `sandbox.ContainerState` |

### Networking

`ContainerSpec.Network` is currently a single string. For K8s:

| Network value | K8s behavior |
|---|---|
| `"none"` | Apply a deny-all NetworkPolicy to the pod |
| `"host"` | Not supported in multi-tenant (security risk); reject or map to cluster network |
| `"slirp4netns"` | Not applicable; map to default cluster networking |
| (empty/default) | Default cluster networking; egress allowed |

For multi-tenant (multi-tenant), per-tenant NetworkPolicies restrict cross-tenant traffic. This is a multi-tenant concern — this spec just ensures the backend can apply pod-level network restrictions.

### Image Management

Currently `GET /api/images` checks `podman images` locally. For K8s:
- Images are pulled by the kubelet, not managed by wallfacer
- `GET /api/images` can check a container registry (or report "managed by cluster")
- `POST /api/images/pull` becomes a no-op or triggers a preflight pod to warm the image on a node

### Resource Limits

`ContainerSpec.CPUs` and `ContainerSpec.Memory` map directly to `resources.limits`. The K8s backend can also set `resources.requests` to a fraction of limits for bin-packing efficiency.

Optional: per-tenant resource quotas via K8s `ResourceQuota` objects (multi-tenant concern).

---

## Implementation Tasks

| # | Task | Depends on | Effort |
|---|------|-----------|--------|
| 1 | Implement `K8sBackend.Launch()` — Job creation with hot tier mounts | Tenant FS | Large |
| 2 | Implement `k8sHandle` — Wait, Stop, State via pod watch | 1 | Medium |
| 3 | Implement `k8sHandle.Logs()` — streaming pod logs | 2 | Small |
| 4 | Implement `k8sHandle.Exec()` — exec into running pod | 2 | Medium |
| 5 | Volume mount translation — host paths to fs.latere.ai hot tier paths | Tenant FS | Medium |
| 6 | Network policy support — apply deny-all for `Network: "none"` | 1 | Small |
| 7 | Image management API — adapt `GET /api/images` for K8s | 1 | Small |
| 8 | Add `k8s` as `WALLFACER_SANDBOX_BACKEND` value; config wiring | 1 | Small |
| 9 | Integration tests with kind or minikube | 1–8 | Large |

**New dependency:** `k8s.io/client-go`

---

## Dependencies

- **Sandbox Backend Interface** — complete. Implements `sandbox.Backend` and `sandbox.Handle`.
- **Tenant Filesystem** — provides the fs.latere.ai hot tier layout (repos, worktrees, config) that this backend mounts into pods.

## What depends on this

- **Multi-Tenant** — the control plane configures `K8sBackend` per tenant (namespace, hot path root, resource quotas).

## Deferred: Remote Docker Backend

A simpler alternative for single-host remote setups. Implements `sandbox.Backend` via Docker client SDK over SSH/TLS. Lower priority than K8s. Can be added as a separate spec if demand arises.
