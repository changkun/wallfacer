#!/usr/bin/env bash
#
# Build wallfacer CLI release binaries for all target platforms.
#
# Invoked by the central release pipeline's build_cli step (latere-ai/ci),
# which provides Go and Bun. Produces dist/wallfacer-<os>-<arch>[.exe], which
# the pipeline attaches to the GitHub release. Repo-specific build logic (ssg
# frontend, SPA embed, version + sandbox-tag ldflags, GOOS/GOARCH matrix) lives
# here, not in the shared workflow.
#
# Env: GITHUB_REF_NAME (the vX.Y.Z tag) is supplied by Actions.

set -euo pipefail

cd "$(dirname "$0")/../.."   # repo root

# Build the SSG frontend so main.go's go:embed all:frontend/dist ships the real SPA.
( cd frontend && bun install --frozen-lockfile && bun run build )
test -f frontend/dist/index.html

# Sandbox image tag baked into the CLI: latest release of latere-ai/images,
# falling back to the highest semver tag if the releases API is unavailable.
SANDBOX_TAG=$(curl -sf https://api.github.com/repos/latere-ai/images/releases/latest 2>/dev/null | jq -r '.tag_name // empty')
if [ -z "$SANDBOX_TAG" ]; then
  SANDBOX_TAG=$(git ls-remote --tags https://github.com/latere-ai/images 2>/dev/null \
    | awk -F/ '{print $NF}' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1)
fi
[ -n "$SANDBOX_TAG" ] || { echo "could not resolve latere-ai/images tag" >&2; exit 1; }

VERSION="${GITHUB_REF_NAME#v}"
LDFLAGS="-s -w -X latere.ai/x/wallfacer/internal/cli.Version=${VERSION} -X latere.ai/x/wallfacer/internal/cli.SandboxTag=${SANDBOX_TAG}"

mkdir -p dist
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
  goos="${target%/*}"; goarch="${target#*/}"; ext=""
  [ "$goos" = "windows" ] && ext=".exe"
  out="dist/wallfacer-${goos}-${goarch}${ext}"
  echo "building $out (SandboxTag=$SANDBOX_TAG)"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "$LDFLAGS" -o "$out" .
done

ls -la dist/
