#!/usr/bin/env bash
# ui-test.sh — boot wallfacer against deterministic demo data and assert UI
# invariants (checks.mjs) to catch render-crash and broken-layout regressions
# that jsdom unit tests cannot. Exits non-zero when any check fails, so it works
# as a CI gate or `make ui-test`.
#
#   ./frontend/scripts/ui-shots/ui-test.sh            # full: build, seed, boot, check
#   SKIP_BUILD=1 ./frontend/scripts/ui-shots/ui-test.sh   # reuse an existing ./wallfacer + dist
#
# Playwright lives in a throwaway /tmp sandbox (never under frontend/, which
# would break the vite-ssg build), mirroring regen.sh.
set -euo pipefail

cd "$(dirname "$0")/../../.."   # repo root

PORT="${PORT:-8097}"
DATA=/tmp/wf-uitest-data
HOME_DIR=/tmp/wf-uitest-home
WS=/tmp/wf-uitest-ws
PW=/tmp/ui-shots-pw
BASE="http://localhost:$PORT"

if [ "${SKIP_BUILD:-}" != "1" ]; then
  echo "==> Building SPA + binary (embeds current UI)"
  make frontend-build
  go build -o wallfacer .
fi

echo "==> Seeding deterministic demo data"
rm -rf "$HOME_DIR"   # fresh config home so migration + first-run are exercised
node frontend/scripts/ui-shots/seed.mjs --data "$DATA" --home "$HOME_DIR" --ws "$WS" >/dev/null

echo "==> Ensuring playwright sandbox at $PW"
mkdir -p "$PW"
if [ ! -d "$PW/node_modules/playwright" ]; then
  (cd "$PW" && npm install --silent playwright@latest >/dev/null)
fi
# Idempotent: installs the chromium browser binary if missing, no-op otherwise.
(cd "$PW" && npx --yes playwright install chromium >/dev/null 2>&1 || true)
cp frontend/scripts/ui-shots/checks.mjs "$PW/checks.mjs"

echo "==> Booting wallfacer on :$PORT"
HOME="$HOME_DIR" ./wallfacer run -data "$DATA" -addr ":$PORT" -no-browser >/tmp/wf-uitest-server.log 2>&1 &
SERVER_PID=$!
trap 'kill "$SERVER_PID" 2>/dev/null || true' EXIT
ready=0
for _ in $(seq 1 40); do
  if curl -sf "$BASE/" >/dev/null 2>&1; then ready=1; break; fi
  sleep 1
done
if [ "$ready" != "1" ]; then
  echo "server failed to come up; tail of log:" >&2
  tail -20 /tmp/wf-uitest-server.log >&2 || true
  exit 1
fi

echo "==> Running UI regression checks"
set +e
node "$PW/checks.mjs" --base "$BASE" "$@"
CODE=$?
set -e
exit "$CODE"
