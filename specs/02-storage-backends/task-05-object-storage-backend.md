# Task 5: Implement ObjectStorageBackend (S3/GCS)

**Status:** Not started
**Depends on:** Task 2
**Effort:** Medium

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

2. Implement `SaveOutput` and `ReadOutput`:
   - Key layout: `<prefix>/<task-uuid>/outputs/<filename>`
   - Use multipart upload for large files

3. All other `StorageBackend` methods return `ErrNotSupported` — structured data is handled by the database backend.

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
