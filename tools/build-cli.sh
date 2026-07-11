#!/usr/bin/env bash
#
# Build wallfacer's CLI release binaries into dist/. Invoked by the shared
# release pipeline (latere-ai/ci) as its `cli_build_cmd` — the pipeline runs
# this in the repo with Go + Bun available and attaches dist/* to the GitHub
# release. Keeping the platform matrix, frontend embed, SANDBOX_TAG resolution,
# and version ldflags here (not in a workflow) is exactly what cli_build_cmd is
# for.
#
# Reproduces the pre-unification release.yml `binary` job verbatim.
set -euo pipefail

VERSION="${GITHUB_REF_NAME#v}"

# The `wallfacer web` server binary embeds the built frontend via
# main.go's `//go:embed all:frontend/dist`, so build it before `go build`.
( cd frontend && bun install --frozen-lockfile && bun run build )

# SANDBOX_TAG source of truth: latest release of github.com/latere-ai/images.
# Fall back to the highest vMAJOR.MINOR.PATCH git tag if the releases API is
# unavailable (rate limit, transient failure).
tag=$(curl -sf https://api.github.com/repos/latere-ai/images/releases/latest 2>/dev/null | jq -r '.tag_name // empty')
if [ -z "$tag" ]; then
  tag=$(git ls-remote --tags https://github.com/latere-ai/images 2>/dev/null | awk -F/ '{print $NF}' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1)
fi
if [ -z "$tag" ]; then
  echo "Could not resolve latere-ai/images tag" >&2
  exit 1
fi
SANDBOX_TAG="$tag"

ldflags="-s -w -X latere.ai/x/wallfacer/internal/cli.Version=${VERSION} -X latere.ai/x/wallfacer/internal/cli.SandboxTag=${SANDBOX_TAG}"

mkdir -p dist
build() {
  local goos="$1" goarch="$2" ext="${3:-}"
  echo "building wallfacer-${goos}-${goarch}${ext} (version=${VERSION}, sandbox=${SANDBOX_TAG})"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "$ldflags" \
    -o "dist/wallfacer-${goos}-${goarch}${ext}" .
}

build linux  amd64
build linux  arm64
build darwin amd64
build darwin arm64
build windows amd64 .exe

echo "built:"; ls -1 dist/
