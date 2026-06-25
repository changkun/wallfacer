---
title: Windows Support — Tier 2 (Native Windows Host)
status: archived
depends_on:
  - specs/foundations/sandbox-backends.md
affects:
  - internal/sandbox/
effort: medium
created: 2026-03-22
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---


# Windows Support — Tier 2 (Native Windows Host)

## Summary

Tier 2 Windows support is complete. The Go server runs natively on Windows
hosts with Linux sandbox containers via Docker Desktop or Podman Desktop.

## What Was Implemented

- Cross-platform signal handling, `execve` replacement, SELinux mount stripping,
  browser/file-manager launch, container runtime detection, path separator handling
- Windows CI: build + vet + unit tests (`.github/workflows/test.yml`)
- Windows release binaries (`windows/amd64` in `.github/workflows/release-binary.yml`;
  `install.sh` supports Windows)
- Native Windows getting-started docs (`docs/guide/getting-started.md`)
- Container path translation: Windows drive-letter paths translated for Docker
  (`C:\` → `/c/`) and Podman (`C:\` → `/mnt/c/`) in `internal/sandbox/spec.go`

## Non-Goals

- Windows ARM64 support (revisit if demand appears)
- Native Windows containers (Claude Code requires Linux)
- GUI installer / MSI package (`go build` or pre-built binary suffices)
- Windows-specific sandbox images (containers are always Linux)
- E2E testing on Windows (users are expected to run via WSL2; native Windows
  is best-effort with unit test coverage only)
