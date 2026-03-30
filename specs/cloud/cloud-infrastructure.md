---
title: Cloud Infrastructure
status: drafted
depends_on: []
affects: [deploy/]
effort: xlarge
created: 2026-03-28
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Cloud Infrastructure

## Problem

Tenant filesystem, K8s sandbox, and cloud storage tasks (PG + S3) define what wallfacer needs from cloud infrastructure — but not how that infrastructure is provisioned across different cloud providers. Each provider offers managed equivalents of K8s, PostgreSQL, S3-compatible storage, and block volumes, but provisioning and configuration differ.

Wallfacer needs to support:
- **DigitalOcean** — primary target for personal/small-team use (lower cost, simpler ops)
- **AWS** — enterprise deployments, on-prem (EKS Anywhere)
- **GCP** — enterprise deployments
- **Alibaba Cloud** — enterprise deployments in China/APAC
- **Self-hosted / on-prem** — bare metal K8s with self-managed PG and MinIO

Without a clear infrastructure strategy, each deployment becomes a one-off, and operational knowledge doesn't transfer between providers.

## Architecture: Two Layers

Wallfacer's cloud deployment separates cleanly into two layers:

```
┌──────────────────────────────────────────────────────┐
│  Application Layer (cloud-agnostic)                  │
│                                                      │
│  wallfacer binary ──▶ K8s API (sandbox pods)         │
│                  ──▶ PG wire protocol (task data)     │
│                  ──▶ S3 API (blobs)                   │
│                  ──▶ PVC (tenant volumes)              │
│                                                      │
│  No cloud-specific code. Talks to standard APIs.     │
└──────────────────────────────────┬───────────────────┘
                                   │
                                   │ configured via env vars / K8s manifests
                                   │
┌���─────────────────────────────────▼───────────────────┐
│  Infrastructure Layer (per-cloud IaC modules)        │
│                                                      │
│  Provisions managed services that expose those APIs: │
│  K8s cluster, managed PG, S3 bucket, PVCs, DNS, TLS │
│                                                      │
│  One module per cloud provider.                      │
└──────────────────────────────────────────────────────┘
```

**The application layer does not change across clouds.** `K8sBackend` talks to the K8s API regardless of whether it's DOKS, EKS, or bare-metal. The PG `StorageBackend` connects via a DSN. The S3 blob backend uses an endpoint URL. All cloud-specific decisions are in the infrastructure layer.

---

## Application-Layer Abstractions

These are already defined in other specs. Summarized here to show why no cloud-specific application code is needed.

### Compute: Kubernetes API

`K8sBackend` uses `client-go` to create Jobs, watch pods, stream logs, and exec into containers. Every managed K8s offering exposes the same API. The backend needs:

- **Kubeconfig** — provided by the managed K8s service or via in-cluster config when wallfacer runs as a pod
- **Namespace** — per-tenant or shared
- **StorageClass name** — for PVC provisioning (cloud-specific, passed as config)

### Storage: PostgreSQL Wire Protocol

The PG `StorageBackend` connects via `DATABASE_URL`. Every managed PG service speaks the same protocol. The backend needs:

- **DSN** — `postgres://user:pass@host:5432/wallfacer?sslmode=require`
- **Connection pooling** — managed services may provide PgBouncer or equivalent; the application uses `database/sql` with pool settings

### Blobs: S3-Compatible API

The S3 `ObjectStorageBackend` uses the S3 API. Most providers offer S3-compatible endpoints:

| Provider | Service | S3 compatible? | Notes |
|----------|---------|----------------|-------|
| DigitalOcean | Spaces | Yes | Native S3 API |
| AWS | S3 | Yes | The original |
| GCP | GCS | Partial | S3 interop mode via `storage.googleapis.com`; or use GCS client directly |
| Alibaba | OSS | Yes | S3-compatible API endpoint |
| Self-hosted | MinIO | Yes | Drop-in S3 replacement |

For GCS, two options: (a) use S3 interop mode (simpler, slight feature gaps), or (b) add a `GCSBackend` that implements `ObjectStorageBackend` using the GCS client. Start with S3 interop; add native GCS only if needed.

The blob backend needs:
- **Endpoint URL** — `https://<region>.digitaloceanspaces.com`, `https://s3.<region>.amazonaws.com`, etc.
- **Bucket name**
- **Access key + secret key** (or IAM role for AWS/GCP)

### Volumes: PVC via StorageClass

Tenant volumes are Kubernetes PVCs. The cloud-specific part is the `StorageClass`:

| Provider | StorageClass driver | Volume type |
|----------|-------------------|-------------|
| DigitalOcean | `dobs.csi.digitalocean.com` | DO Volumes (block) |
| AWS | `ebs.csi.aws.com` | EBS (gp3/io2) |
| GCP | `pd.csi.storage.gke.io` | Persistent Disk (pd-ssd) |
| Alibaba | `diskplugin.csi.alibabacloud.com` | Cloud Disk |
| Self-hosted | `rancher.io/local-path` or NFS | Local SSD or NFS |

The wallfacer application never references StorageClass names — it creates PVCs that reference a StorageClass configured in the K8s cluster. The IaC module sets up the correct StorageClass.

---

## Infrastructure Layer: Per-Cloud IaC Modules

Each cloud provider gets a Terraform (OpenTofu) module that provisions the full stack. The modules live in `deploy/<provider>/`.

### Module Structure

```
deploy/
├── base/                          # Shared K8s manifests (Helm chart or Kustomize)
│   ├── wallfacer-server/          # Server Deployment, Service, PVC templates
│   ├── control-plane/             # Control plane Deployment
│   └── common/                    # RBAC, NetworkPolicies, StorageClass references
│
├── digitalocean/                  # DO-specific Terraform
│   ├── main.tf                    # DOKS cluster, managed PG, Spaces bucket
│   ├── variables.tf               # Region, node size, PG plan, etc.
│   ├── outputs.tf                 # Kubeconfig, DSN, S3 endpoint
│   └── README.md
│
├── aws/                           # AWS-specific Terraform
│   ├── main.tf                    # EKS, RDS, S3, IAM roles
│   ├── variables.tf
│   ├── outputs.tf
│   └── README.md
│
├── gcp/                           # GCP-specific Terraform
│   ├── main.tf                    # GKE, Cloud SQL, GCS
│   ├── variables.tf
│   ├── outputs.tf
│   └── README.md
│
├── alibaba/                       # Alibaba Cloud Terraform
│   ├── main.tf                    # ACK, ApsaraDB, OSS
│   ├── variables.tf
│   ├── outputs.tf
│   └── README.md
│
└── self-hosted/                   # Bare-metal / on-prem
    ├── main.tf                    # Optional: provisions MinIO, PG on existing K8s
    ├── variables.tf
    └── README.md
```

### What Each Module Provisions

| Resource | Purpose | Used by |
|----------|---------|---------|
| Managed K8s cluster | Sandbox pod execution, server hosting | K8s sandbox, multi-tenant |
| Managed PostgreSQL | Task data storage | Cloud storage |
| S3-compatible bucket | Blob storage (outputs, oversight) | Cloud storage |
| Container registry | Sandbox images (Claude, Codex) | K8s sandbox |
| Block storage (StorageClass) | Tenant PVCs | Tenant filesystem |
| Load balancer + TLS cert | HTTPS ingress | Multi-tenant |
| DNS record | `wallfacer.example.com` | Multi-tenant |
| IAM / service accounts | K8s ↔ cloud service auth | All |

### Module Outputs → Application Config

Each Terraform module outputs the values the application needs:

```hcl
output "kubeconfig" {
  description = "Kubeconfig for the K8s cluster"
  value       = module.k8s.kubeconfig
  sensitive   = true
}

output "database_url" {
  description = "PostgreSQL connection string"
  value       = "postgres://${module.pg.user}:${module.pg.password}@${module.pg.host}:${module.pg.port}/wallfacer?sslmode=require"
  sensitive   = true
}

output "s3_endpoint" {
  description = "S3-compatible endpoint URL"
  value       = module.storage.endpoint
}

output "s3_bucket" {
  description = "Blob storage bucket name"
  value       = module.storage.bucket_name
}

output "storage_class" {
  description = "K8s StorageClass name for tenant PVCs"
  value       = module.k8s.storage_class_name
}

output "registry_url" {
  description = "Container registry URL for sandbox images"
  value       = module.registry.url
}
```

These outputs are injected into the wallfacer server as environment variables (via K8s ConfigMap/Secret), connecting the infrastructure layer to the application layer.

---

## Provider-Specific Notes

### DigitalOcean (primary target)

**Advantages:** Simple pricing, flat-rate Spaces ($5/mo for 250GB), managed K8s (DOKS) with straightforward setup, managed PG available in all regions.

**Constraints:**
- DOKS node pools: max 100 nodes per pool (sufficient for early deployment)
- Spaces: S3-compatible but no lifecycle policies via S3 API (use DO API for TTL rules)
- DO Volumes: max 16TB per volume, ReadWriteOnce only (no ReadWriteMany — confirms tenant filesystem pod affinity recommendation)
- No managed Redis (use in-cluster if needed for control plane sessions)

**Sizing estimate (small deployment, ~10 tenants):**
- K8s: 1 node pool, 3× s-4vcpu-8gb ($48/mo each)
- PG: db-s-2vcpu-4gb ($60/mo)
- Spaces: 1 bucket ($5/mo)
- Total: ~$210/mo

### AWS

**Advantages:** Broadest service catalog, EKS Anywhere for on-prem, IAM roles for service accounts (no static credentials), S3 lifecycle rules, RDS Multi-AZ.

**Constraints:**
- EKS control plane: $73/mo baseline
- NAT Gateway costs for private subnets (~$32/mo + data transfer)
- EBS gp3: $0.08/GB/mo, io2 for high IOPS
- Consider Fargate for sandbox pods (no node management, but no GPU and higher per-pod cost)

**Enterprise features:**
- VPC peering for on-prem database access
- PrivateLink for S3 (no data transfer cost within VPC)
- AWS Secrets Manager for credential injection

### GCP

**Advantages:** GKE Autopilot (fully managed nodes), Cloud SQL with automated backups, Workload Identity for credential-free service auth.

**Constraints:**
- GCS S3 interop: supports most operations but not multipart upload abort or all ACL features. Test with wallfacer's blob operations to confirm compatibility, or use native GCS client.
- Persistent Disk: ReadWriteOnce only for standard PD; Filestore for ReadWriteMany (expensive)

### Alibaba Cloud

**Advantages:** Required for China/APAC deployments, ACK (managed K8s) is mature, OSS is fully S3-compatible, ApsaraDB PG available.

**Constraints:**
- China regions require ICP filing for public-facing services
- OSS S3-compatible endpoint: `https://<bucket>.<region>.aliyuncs.com` (different from AWS S3 URL pattern — application must accept configurable endpoint)
- Cross-region replication available but adds complexity

### Self-Hosted / On-Prem

For enterprises that can't use public cloud. Requires existing K8s cluster (k3s, kubeadm, Rancher, etc.).

**Stack:**
- K8s: existing cluster
- PostgreSQL: operator-managed (CloudNativePG, Zalando PG Operator) or external
- S3: MinIO (single-binary, S3-compatible, easy to operate)
- Volumes: local-path provisioner or NFS CSI driver
- TLS: cert-manager with Let's Encrypt or internal CA
- Registry: Harbor or cluster-local registry

The self-hosted module is the simplest Terraform — it provisions MinIO and PG within the existing cluster (or accepts external endpoints).

---

## Implementation Approach

### Phase 1: DigitalOcean (primary target)

Build the DO module first. This validates the IaC pattern and produces a working reference deployment.

1. Write `deploy/digitalocean/` Terraform module
2. Write `deploy/base/` K8s manifests (Helm chart or Kustomize)
3. Document the deployment workflow: `terraform apply` → `kubectl apply` → wallfacer running
4. Test full lifecycle: provision → deploy → create tenant → run task → hibernate → wake → destroy

### Phase 2: Self-Hosted

Second because it's the simplest infra-wise (no cloud API) and validates that the base K8s manifests work without managed services.

### Phase 3: AWS / GCP / Alibaba

Enterprise modules. Each reuses `deploy/base/` manifests and adds provider-specific Terraform for managed services. Prioritize based on demand.

---

## Implementation Tasks

| # | Task | Depends on | Effort |
|---|------|-----------|--------|
| 1 | Write `deploy/base/` K8s manifests (server, control-plane, RBAC, NetworkPolicy) | K8s sandbox, multi-tenant | Medium |
| 2 | Write `deploy/digitalocean/` Terraform module | 1 | Medium |
| 3 | End-to-end deployment test on DO | 2 | Medium |
| 4 | Write `deploy/self-hosted/` module (MinIO + PG in-cluster) | 1 | Small |
| 5 | Write `deploy/aws/` Terraform module | 1 | Medium |
| 6 | Write `deploy/gcp/` Terraform module | 1 | Medium |
| 7 | Write `deploy/alibaba/` Terraform module | 1 | Medium |
| 8 | Deployment documentation per provider | 2–7 | Medium |

Tasks 5–7 are independent and can be built in parallel.

---

## Dependencies

- **Tenant Filesystem** — defines the PVC layout that StorageClass provisions
- **K8s Sandbox Backend** — defines the K8s resource types (Jobs, PVCs, NetworkPolicies) that IaC must support
- **Cloud Storage** — defines PG schema and S3 bucket structure
- **Multi-Tenant** — defines control plane deployment, DNS, TLS, auth

This spec can begin in parallel with tenant filesystem/K8s sandbox (base manifests evolve alongside the application), but full end-to-end testing requires all of tenant FS + K8s sandbox + cloud storage + multi-tenant.

## What depends on this

Nothing — this is a leaf spec. It enables deployment but no other milestone depends on it.
