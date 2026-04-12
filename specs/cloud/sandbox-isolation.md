---
title: Sandbox Isolation & Policy Engine
status: drafted
depends_on:
  - specs/cloud/k8s-sandbox.md
  - specs/cloud/tenant-filesystem.md
  - specs/cloud/multi-tenant.md
affects:
  - internal/sandbox/
  - internal/runner/
effort: large
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Sandbox Isolation & Policy Engine

## Problem

In cloud-hosted mode (`specs/cloud/multi-tenant.md`), the sandbox is a stateless, full-OS runtime where an autonomous agent can install tools, run commands, and touch the network. That capability is load-bearing for the product — agents need to do real work — but multi-tenancy means any single agent's actions could, in principle, reach another tenant's data, exfiltrate credentials, or abuse the network. This spec defines the **policy engine** that monitors and controls the sandbox's action space so agent autonomy doesn't become tenant blast radius.

Local mode (`internal/sandbox/local.go`) is out of scope. Local users have implicit authority over their own machine; adding a policy engine there is not justified by threat model.

## Goals

1. **Agent autonomy.** The agent decides what to install and run. No pre-declaration of tools, no approval loop.
2. **Policy enforcement.** Dangerous actions (exfiltration, cross-tenant reach, SSRF) are blocked before they land, not just logged after.
3. **Full observability.** Every network request, every filesystem write outside the mount, every privileged syscall attempt is recorded. The activity log is consumable by the user (for transparency) and the control plane (for anomaly detection).
4. **Low friction.** Common operations (installing a dependency, cloning the repo, talking to the LLM API) work without explicit allowlist entries.
5. **Multi-tenant safe.** A policy violation in one tenant's sandbox cannot affect another tenant.

## Scope

Three action surfaces, in priority order:

| Surface | What it covers | Priority |
|---------|---------------|----------|
| **Network** | Outbound HTTP(S), DNS, raw TCP/UDP from the sandbox | Must-have |
| **Filesystem** | Writes outside the fs.latere.ai mount; reads from host paths | Must-have |
| **Processes / syscalls** | Privilege escalation, kernel interactions, container escape attempts | Nice-to-have (leverages standard K8s/seccomp) |

An **action log** records all three, flows to the control plane, and is queryable per task and per tenant.

---

## Network Policy

### Model: allow + deny + log

Three categories applied in order:

1. **Allowlist** — known-good destinations, permitted without further evaluation:
   - LLM API endpoints (`api.anthropic.com`, `api.openai.com`, configured gateway URLs)
   - Git hosts for the tenant's configured repos (`github.com`, `gitlab.com`, custom Git hosts per tenant config)
   - Package registries (`registry.npmjs.org`, `pypi.org`, `crates.io`, `index.docker.io`, `ghcr.io`, OS package mirrors)
   - fs.latere.ai (hot tier access, if reached via API rather than mount)
2. **Denylist** — known-bad, blocked regardless:
   - RFC1918 private IP ranges (`10/8`, `172.16/12`, `192.168/16`)
   - Link-local (`169.254/16`), loopback (`127/8`)
   - Cluster-internal service IPs (K8s service CIDR)
   - Known SSRF targets (cloud metadata endpoints: `169.254.169.254`, etc.)
3. **Default** — for destinations in neither list: **allow + log with elevated tag**. This preserves agent autonomy for novel destinations while flagging them for review. Per-tenant policy can tighten the default to "deny" for stricter environments.

### Observability

For every outbound flow the engine records:
- Timestamp, task ID, tenant ID
- Destination (hostname, resolved IP, port)
- Protocol (HTTP method + path for TLS-intercepted flows; SNI + byte counts for pass-through TLS)
- Policy verdict (allow / deny / default-allow-logged)
- Byte counts sent/received
- Parent process inside the sandbox (which binary initiated the connection)

The log is structured JSON, sunk to the control plane's activity store.

### Enforcement points (design choice open)

Three implementation options, not yet chosen:

| Option | How | Tradeoffs |
|--------|-----|-----------|
| **Egress proxy** (Envoy / Squid / custom) | Pod-level sidecar intercepts all egress; agent's HTTPS traffic is MITM'd with an injected CA cert for L7 visibility | Rich L7 policy; requires CA cert trust in sandbox; adds latency |
| **K8s NetworkPolicy + flow logs** | CNI-level L3/L4 allow/deny; flow logs from the CNI (Cilium, Calico) | No L7 visibility; simpler; faster |
| **eBPF-based control + audit** | In-kernel program attaches to sockets; full visibility + control | Most flexible and performant; higher implementation complexity; CNI-dependent |

**Likely path:** Start with #2 (NetworkPolicy + flow logs) to get basic containment, then layer #1 (proxy) for L7 policy when the threat model demands it. eBPF is the long-term direction when volume justifies the operational cost.

---

## Filesystem Policy

### Model: workspace-RW, everything-else-readonly

- The fs.latere.ai mount (e.g., `/workspace/<repo>`) is RW.
- The rest of the container filesystem is **read-only** at the container boundary (`readOnlyRootFilesystem: true` on the pod spec).
- Tmpfs mounts for `/tmp`, `/run`, package manager caches — these are RW but ephemeral and bounded in size.
- Tool installation (`apt install …`) writes to read-only root, which fails by default. Two options:
  - (a) Allow `/usr/local`, `/opt` to be RW via a small writable overlay. Simple; allows `pip install --user`, `npm install -g`, but not `apt install`.
  - (b) Switch `readOnlyRootFilesystem: false` and rely on ephemeral container lifetime + policy audit. Matches "autonomy" principle better; slightly weaker isolation.

**Likely path:** (b) — the sandbox is destroyed after every task anyway, so root-FS mutations are meaningless beyond the task lifetime. The stateless primitive makes root-FS isolation cheap to skip.

### What is logged

- Every write to a path outside the fs.latere.ai mount (when the container still has a read-only root policy).
- Every read of a file path that looks sensitive (`/etc/shadow`, `/proc/*/environ`, etc.) — for audit, even though reads are permitted on read-only root.

---

## Process / Syscall Policy

Standard hardening, not a custom policy engine:

- `seccomp` profile blocking dangerous syscalls (`ptrace`, `kexec_*`, `bpf` unless specifically needed).
- `NoNewPrivileges`, dropped Linux capabilities (no `CAP_NET_ADMIN`, `CAP_SYS_ADMIN`, etc.).
- Non-root user inside the container.
- Pod Security Standard: `restricted`.

If the agent needs elevated privileges for a specific tool, that's a policy escalation that goes through the control plane, not a sandbox-local decision.

---

## Action Log

### Format

Structured JSON, one event per line:

```json
{
  "ts": "2026-04-12T10:23:45.123Z",
  "task_id": "<uuid>",
  "tenant_id": "<uuid>",
  "surface": "network",
  "verdict": "allow",
  "category": "allowlist",
  "detail": {
    "host": "api.anthropic.com",
    "method": "POST",
    "path": "/v1/messages",
    "bytes_out": 8421,
    "bytes_in": 15234
  },
  "process": "claude-code"
}
```

`surface` ∈ {`network`, `filesystem`, `process`}.
`verdict` ∈ {`allow`, `deny`, `default_allow_logged`}.

### Sinks

- **Primary:** control plane action store (for per-tenant queries, anomaly detection, billing-relevant bytecount)
- **Per-task log** streamed to the wallfacer task event log so the user sees network activity next to their task timeline
- **Metrics:** aggregate byte counts and deny counts exported to Prometheus

### Retention

- Per-task detail log: retained as long as the task's trace (existing retention policy)
- Aggregate metrics: long-term in Prometheus
- Per-tenant anomaly records: retained as long as the tenant account exists

---

## Configuration

Per-tenant policy overrides (control plane state):

| Field | Default | Description |
|-------|---------|-------------|
| `network.default_verdict` | `allow_logged` | Verdict for destinations in neither allowlist nor denylist |
| `network.extra_allow` | `[]` | Additional hostnames / CIDRs permitted |
| `network.extra_deny` | `[]` | Additional hostnames / CIDRs blocked |
| `filesystem.root_writable` | `true` | Whether root FS is writable (supports tool installs) |
| `process.seccomp_profile` | `restricted` | Seccomp profile name |

Tenants on stricter tiers (enterprise, compliance-bound) can set `network.default_verdict: deny` and provide an explicit allowlist.

---

## Implementation Order

1. **K8s NetworkPolicy + flow logs** — baseline L3/L4 containment and observability; no custom code
2. **Action log format + sink** — define the event schema; ingest CNI flow logs into the control plane store
3. **Filesystem read-only root + tmpfs mounts** — harden the pod spec
4. **seccomp profile + pod security** — standard K8s hardening
5. **Egress proxy for L7 visibility** — add Envoy sidecar or equivalent; TLS interception with injected CA
6. **Per-tenant policy overrides** — UI and API for tenants to customize allowlist/denylist
7. **Anomaly detection** — consume the action log; flag tasks with unusual egress patterns

Steps 1–4 are the must-have containment baseline. Steps 5–7 are the richer observability and customization layer.

---

## Open Questions

- **TLS interception:** Does injecting a CA cert into the sandbox so the proxy can MITM HTTPS violate any agent harness assumptions (e.g., does Claude Code or Codex pin certs)?
- **Package-registry caching:** Should we run a cluster-local cache (Artifactory, Verdaccio) to reduce egress volume and make denylist of package sources possible without breaking `npm install`?
- **Policy exceptions:** If the agent legitimately needs to reach a non-allowlisted host for a task (e.g., calling a partner API), what's the request/approval flow? Control plane API? Manual allowlist edit? One-time task-scoped exception?
- **Local-mode adoption:** Do we eventually offer the policy engine as an opt-in for local authenticated mode, for users who want the same guarantees on their laptop? Deferred.

---

## Dependencies

- **K8s Sandbox** (`specs/cloud/k8s-sandbox.md`) — the sandbox runtime this policy engine attaches to
- **Tenant Filesystem** (`specs/cloud/tenant-filesystem.md`) — defines the fs.latere.ai mount that the FS policy treats as the only RW area
- **Multi-Tenant** (`specs/cloud/multi-tenant.md`) — defines the tenant identity that scopes per-tenant policy overrides

## What depends on this

Nothing directly — this is a containment layer. But the stateless-sandbox frame in `multi-tenant.md` assumes this engine exists.
