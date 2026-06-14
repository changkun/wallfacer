---
title: Local-First Build and Deploy (wallfacerd)
status: archived
depends_on: []
affects:
  - Makefile
  - .github/workflows/wallfacerd.yml
  - .github/workflows/deploy-wallfacerd.yml
  - README.md
  - CLAUDE.md
  - DEPLOY_LOG.md
effort: small
trigger: parent umbrella spec; wallfacerd image and prod deploy only (desktop/binary releases unchanged)
created: 2026-05-31
updated: 2026-06-14
author: changkun
dispatched_task_id: null
---

> **Abandoned 2026-06-01.** Implemented and worked end-to-end, then reverted.
> The local flow serialised build / push / deploy on the dev machine; GitHub
> Actions runs them asynchronously off the laptop. Cross-arch image builds
> (amd64 cluster from arm64 Apple Silicon) also work reliably in GHA without
> the local qemu / BUILDPLATFORM workarounds. Restored
> `.github/workflows/{wallfacerd,deploy-wallfacerd}.yml`. The
> release-evidence pattern (smoke + evidence body + GitHub release) is
> being lifted into GitHub Actions for every repo as a follow-up.

# Local-First Build and Deploy (wallfacerd)

## Overview

Tier B child of [[local-build-deploy]]. Moves `wallfacerd` (the web/server image) build + push + k8s deploy from GH Actions to local `make release` + `make deploy`. The two non-server release flows in this repo (`release-binary.yml` and `release-desktop.yml`) are explicitly out of scope - Wails desktop signing/notarization needs Apple/Windows keys that aren't worth relocating.

This spec lives in the `local` track (`specs/local/`) so it appears in the spec tree (it is archived, shown when "Show archived" is on). The cross-repo umbrella ([[local-build-deploy]] in `terraform/specs/`) remains the overall index for the multi-repo migration; the former `depends_on` edges to the sibling `auth/` and `terraform/` repos were dropped from the frontmatter because the wallfacer validator cannot resolve cross-repo paths.

## Current state

- `.github/workflows/test.yml` - multi-OS test matrix. Already test-only; keep.
- `.github/workflows/wallfacerd.yml` - three jobs: `verify-frontend` (vue-tsc + vite-ssg build), `verify-go` (go build/vet), `build` (Dockerfile.wallfacerd → `ghcr.io/changkun/wallfacerd:<tag>` on push). The verify jobs stay; the `build` job is removed.
- `.github/workflows/deploy-wallfacerd.yml` - fires after `wallfacerd` succeeds on a `v*` ref. `doctl kubeconfig save`, `kubectl apply -f deploy/prod/{service,ingress,deployment}.yaml`, `kubectl -n latere set image deployment/wallfacerd wallfacerd=ghcr.io/changkun/wallfacerd:${TAG#v}`, `rollout status --timeout=120s`. **Deleted entirely.**
- `.github/workflows/release-binary.yml` - goreleaser-style multi-platform binaries on tag. **Keep unchanged.**
- `.github/workflows/release-desktop.yml` - Wails desktop builds with macOS notarization + Windows signtool. **Keep unchanged.**
- `Makefile` - `build`, `build-binary`, `lint`, `fmt`, `test`, `server`, `ui-css`, `api-contract`, `e2e-lifecycle`, etc. No docker/deploy targets.
- `deploy/prod/` - `deployment.yaml`, `ingress.yaml`, `service.yaml`.

Two wallfacer-specific wrinkles:
1. Image lives at `ghcr.io/changkun/wallfacerd` (changkun user, not latere-ai org). `make release` must push there.
2. The image tag in `deploy-wallfacerd.yml` strips the leading `v` (`TAG="${REF#v}"`), so the image is tagged `0.0.7-alpha.6` while the git tag is `v0.0.7-alpha.6`. The Makefile preserves this normalization: image tag = `$(VERSION:v%=%)`.

## Acceptance criteria

1. `Makefile` gains the standard two-entry surface:
   - `make release VERSION=v1.2.3` runs the frontend build (vite-ssg), embeds `frontend/dist` into `internal/webserver/spa/dist`, then `docker build -f Dockerfile.wallfacerd -t ghcr.io/changkun/wallfacerd:1.2.3 .` and pushes. Idempotent.
   - `make deploy VERSION=v1.2.3` verifies the tag exists in ghcr.io, then `kubectl apply -f deploy/prod/{service,ingress,deployment}.yaml`, `kubectl -n latere set image deployment/wallfacerd wallfacerd=ghcr.io/changkun/wallfacerd:1.2.3`, `kubectl -n latere rollout status deployment/wallfacerd --timeout=120s`, appends to `DEPLOY_LOG.md`.
   - `ghcr-login` and `kubeconfig` prereqs as in [[local-build-deploy]]. For wallfacer, `ghcr-login` pushes to `changkun/*` namespace - the `gh auth token` already has the right scope since changkun owns both.
2. `.github/workflows/wallfacerd.yml` is edited: the `build` job (and its needs/permissions/buildx setup) is **removed**. `verify-frontend` and `verify-go` remain.
3. `.github/workflows/deploy-wallfacerd.yml` is **deleted**.
4. `.github/workflows/test.yml`, `release-binary.yml`, `release-desktop.yml` unchanged.
5. `README.md` gains a `## Release` section explaining:
   - Server: `make release` + `make deploy`.
   - Binaries: cut a `v*` tag, `release-binary.yml` runs (unchanged).
   - Desktop: cut a `v*` tag, `release-desktop.yml` runs (unchanged).
6. `CLAUDE.md` § Build & Run Commands gains two lines for `make release` and `make deploy`.
7. `DEPLOY_LOG.md` created.
8. `grep -r DO_TOKEN .github/` returns nothing in this repo.

## Non-goals

- Moving the desktop or binary release flows. Apple notarization and Windows signtool live in GH Actions secrets specifically - out of scope.
- Multi-arch image. wallfacerd is `linux/amd64` only today.
- Changing the image tag scheme (keeping `v`-stripping for backward compatibility with existing manifests and the ghcr.io tag history).

## Implementation notes

- The current `build` job downloads the `frontend-dist` artifact uploaded by `verify-frontend` and copies it into `internal/webserver/spa/dist` before `docker build`. Local `make release` replicates this inline: run the frontend build first (the existing `make build` target already does this), then `docker build`. No artifact passing needed.
- The image tag stripping (`$(VERSION:v%=%)`) is a Make substitution reference. Test it: `VERSION=v0.0.7-alpha.6` → `0.0.7-alpha.6`.
- `deploy-wallfacerd.yml` applies only three manifests (`service.yaml`, `ingress.yaml`, `deployment.yaml`), not the whole `deploy/prod/` directory. Preserve this - there may be a reason (probably nothing else in that dir, but be conservative).

## Doc updates checklist

- [ ] `README.md` - new `## Release` section covering server (make), binaries (tag), desktop (tag).
- [ ] `CLAUDE.md` § Build & Run Commands - add `make release` and `make deploy` lines.
- [ ] `DEPLOY_LOG.md` - new file.

## Verification

1. `cd wallfacer && make test` - passes.
2. `make release VERSION=v0.0.0-pilot` - image at `ghcr.io/changkun/wallfacerd:0.0.0-pilot` (note: no leading `v`).
3. `make deploy VERSION=v0.0.0-pilot` - rollout succeeds; `kubectl -n latere get pods -l app=wallfacerd` shows new tag.
4. `curl -I https://wf.latere.ai/` returns 200.
5. A `v0.0.0-pilot+1` tag push: `wallfacerd.yml` (verify jobs only), `release-binary.yml`, `release-desktop.yml` run; `deploy-wallfacerd.yml` is gone.
6. `grep -r DO_TOKEN .github/` empty.

## Rollback

`git revert` the spec commit; recover the build job in `wallfacerd.yml` and the deleted `deploy-wallfacerd.yml` from history.

## Outcome

Archived (2026-06-14). Implemented and worked end-to-end (`d7c5e39d`), then
deliberately reverted in favor of GitHub Actions (`0ba7b225` "revert: roll
back local-build-deploy migration"). The local flow serialised
build/push/deploy on the dev machine; GHA runs them asynchronously off the
laptop and handles cross-arch builds (amd64 cluster from arm64 Apple
Silicon) without the local qemu / BUILDPLATFORM workarounds.

Reality contradicts every acceptance criterion: `deploy-wallfacerd.yml`
still exists, `wallfacerd.yml` keeps its `build` job, and no `make deploy`
target or `DEPLOY_LOG.md` was kept. The release-evidence pattern (smoke +
evidence body + GitHub release) was lifted into GHA instead (`151e7903`)
rather than into a local `make release`. Status `abandoned` was not a valid
lifecycle value; archived records the deliberate reversal.
