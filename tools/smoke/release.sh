#!/usr/bin/env bash
#
# Post-deploy release smoke checks for wf.latere.ai.
#
# Required tools: curl, grep.
#
# Optional env:
#   BASE_URL          default https://wf.latere.ai
#   TAG               release tag, for evidence output
#   COMMIT            release commit sha, for evidence output
#   BUILD_URL         build link, for evidence output
#   DEPLOY_URL        deploy link, for evidence output
#   OUTPUT_MD         optional path for markdown evidence

set -euo pipefail

BASE_URL="${BASE_URL:-https://wf.latere.ai}"
BASE_URL="${BASE_URL%/}"
TAG="${TAG:-unknown}"
COMMIT="${COMMIT:-unknown}"
BUILD_URL="${BUILD_URL:-}"
DEPLOY_URL="${DEPLOY_URL:-}"
OUTPUT_MD="${OUTPUT_MD:-}"

C_GREEN='\033[0;32m'
C_RED='\033[0;31m'
C_RESET='\033[0m'

pass() { printf "${C_GREEN}OK %s${C_RESET}\n" "$*"; }
fail() { printf "${C_RED}FAIL %s${C_RESET}\n" "$*" >&2; exit 1; }

for cmd in curl grep; do
  command -v "$cmd" >/dev/null || fail "$cmd is required"
done

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

check_status() {
  local label="$1"
  local path="$2"
  local want="$3"
  local out="$4"
  local code
  # --retry rides out the transient 5xx the LB emits during a rollout cutover.
  code=$(curl -sS -L \
    --retry 5 --retry-delay 2 --retry-max-time 30 \
    -o "$out" -w '%{http_code}' "${BASE_URL}${path}") \
    || fail "$label: curl failed"
  if [ "$code" != "$want" ]; then
    fail "$label: expected $want, got $code (body: $(head -c 200 "$out" 2>/dev/null || true))"
  fi
  pass "$label ($code)"
}

# wallfacerd serves the task-board SPA at /. Asset pinning is skipped because
# the frontend builds inside Dockerfile.wallfacerd, so there's no host
# frontend/dist to read the expected hash from. The asset name pattern
# (assets/app-*.js) is checked instead to confirm a real production bundle.
check_status "GET /" "/" "200" "$tmp/index.html"
grep -qi '<!doctype html\|<html' "$tmp/index.html" \
  || fail "GET /: did not return SPA HTML"
asset="$(grep -Eo "assets/app-[^\"'<> ]+\\.js" "$tmp/index.html" | head -1 || true)"
[ -n "$asset" ] || fail "GET /: no Vite entry asset in HTML"
pass "asset present: $asset"

check_status "GET /healthz" "/healthz" "200" "$tmp/healthz"
check_status "GET /api/debug/health" "/api/debug/health" "200" "$tmp/api_health"

if [ -n "$OUTPUT_MD" ]; then
  {
    echo "<!-- release-evidence -->"
    echo
    echo "## Release Evidence"
    echo
    echo "- Tag: \`${TAG}\`"
    echo "- Commit: \`${COMMIT}\`"
    [ -n "$BUILD_URL" ]  && echo "- Build: ${BUILD_URL}"
    [ -n "$DEPLOY_URL" ] && echo "- Deploy: ${DEPLOY_URL}"
    echo "- Asset: \`${asset}\`"
    echo "- Smoke: \`GET /\`, \`/healthz\`, \`/api/debug/health\` returned 200"
  } > "$OUTPUT_MD"
fi

printf "\nrelease smoke passed\n"
