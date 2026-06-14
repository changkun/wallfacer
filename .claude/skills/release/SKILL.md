---
name: release
description: Cut a new wallfacer release by pushing a version tag. The release.yml workflow builds binaries, pushes the image, deploys to k8s, and publishes the GitHub release with generated notes. Usage /release <version> e.g. /release v0.0.7
argument-hint: "<version> e.g. v0.0.7 or v0.0.7-beta"
user-invocable: true
disable-model-invocation: true
---

# Cut a Wallfacer Release

You are cutting a release for wallfacer version `$ARGUMENTS`.

Releases are fully automated by `.github/workflows/release.yml`: pushing a
`v*` tag verifies the build, builds the CLI binaries, pushes the wallfacerd
image, deploys to the `latere` k8s namespace, smokes `wf.latere.ai`, and
publishes the GitHub release with auto-generated notes (the binaries attached).
Your job is only to push a clean, correct tag.

## Pre-flight checks

1. Verify the version argument is provided and starts with `v` (e.g. `v0.0.7`, `v0.0.7-beta`).
2. Verify the working tree is clean (`git status`). Do not sweep unrelated changes into the release.
3. Verify the tag does not already exist (`git tag -l $ARGUMENTS`).
4. Confirm `main` is up to date with origin and CI on the head commit is green (`gh run list --branch main`).

## Tag and push

After confirming the above with the user:

```bash
git tag -a "$ARGUMENTS" -m "$ARGUMENTS"
git push origin "$ARGUMENTS"
```

Tags with a `-suffix` (e.g. `v0.0.7-beta`, `v0.0.7-rc.1`) are published as
pre-releases automatically.

## Verify

- Watch the pipeline: `gh run watch` or `gh run list --workflow release.yml`.
- The `deploy` job must go green before `release` publishes — that means
  `wf.latere.ai` is live on the new image and smoke-passing.
- Print the release URL once published (`gh release view "$ARGUMENTS" --web`).
- Confirm the one-liner install resolves the new tag:
  `curl -fsSL https://raw.githubusercontent.com/changkun/wallfacer/main/install.sh | sh`
