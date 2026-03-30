---
title: "Implement ObjectStorageBackend (S3/GCS)"
status: complete
depends_on:
  - specs/foundations/storage-backends/task-02-filesystem-backend.md
affects:
  - internal/store/backend_blob.go
effort: medium
created: 2026-03-23
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 5: Implement ObjectStorageBackend (S3/GCS)

## Goal

Implement an `ObjectStorageBackend` for large blob storage (turn outputs). Used alongside the database backend for cloud deployments.

## What to do

1. Create `internal/store/backend_blob.go` with `ObjectStorageBackend`:

```go
type ObjectStorageBackend struct {
    bucket string
    client *s3.Client
    prefix string // e.g., "wallfacer/<workspace-key>/"
}

func NewObjectStorageBackend(bucket, prefix, region string) (*ObjectStorageBackend, error)
```

2. Implement blob methods:
   - `SaveBlob` → S3 `PutObject` at key `<prefix>/<task-uuid>/<blob-key>`
   - `ReadBlob` → S3 `GetObject`
   - `DeleteBlob` → S3 `DeleteObject`
   - `ListBlobOwners` → S3 `ListObjectsV2` with prefix scan
   - Use multipart upload for large files

3. All other `StorageBackend` methods (tasks, events) return `ErrNotSupported` — structured data is handled by the database backend via the composite backend.

4. Add env vars to `internal/envconfig/`:
   - `WALLFACER_BLOB_STORAGE` (`s3` | `gcs`)
   - `WALLFACER_BLOB_BUCKET`
   - `WALLFACER_BLOB_REGION`

## New dependency

- `github.com/aws/aws-sdk-go-v2` (S3) or `cloud.google.com/go/storage` (GCS)

## Acceptance criteria

- `ObjectStorageBackend` implements `SaveOutput`/`ReadOutput`
- Integration tests against localstack or minio (skip if unavailable)
- Handles files up to 100 MB without excessive memory use
