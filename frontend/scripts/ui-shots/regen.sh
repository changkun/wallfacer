#!/usr/bin/env bash
# regen.sh — regenerate every committed UI screenshot from a clean demo
# workspace, in both light and dark themes. Run from time to time to keep the
# docs, README, and landing-page shots in sync with the current UI.
#
#   ./frontend/scripts/ui-shots/regen.sh
#
# Pipeline: build the embedded SPA + binary, seed deterministic demo data
# (which mirrors the repo's real specs/ into the workspace), boot wallfacer,
# screenshot each surface at retina 2x, then copy the outputs to their
# committed destinations. Playwright lives in a throwaway /tmp sandbox so it
# never lands in the project (an npm install under frontend/ breaks the build).
set -euo pipefail

cd "$(dirname "$0")/../../.."   # repo root

PORT=8099
DATA=/tmp/wf-demo-data
HOME_DIR=/tmp/wf-demo-home
WS=/tmp/wf-demo-ws
PW=/tmp/ui-shots-pw
OUT=/tmp/wf-shots
BASE="http://localhost:$PORT"

echo "==> Building SPA + binary (embeds current UI)"
make frontend-build
go build -o wallfacer .

echo "==> Seeding demo data (mirrors repo specs/ into the workspace)"
node frontend/scripts/ui-shots/seed.mjs --data "$DATA" --home "$HOME_DIR" --ws "$WS" >/dev/null

echo "==> Ensuring playwright sandbox at $PW"
mkdir -p "$PW"
if [ ! -d "$PW/node_modules/playwright" ]; then
  (cd "$PW" && npm install --silent playwright@latest >/dev/null)
fi
# Run snap.mjs from inside the sandbox so its bare `import 'playwright'`
# resolves (ESM walks up from the script's own dir, and ignores NODE_PATH).
cp frontend/scripts/ui-shots/snap.mjs "$PW/snap.mjs"

echo "==> Booting wallfacer on :$PORT"
HOME="$HOME_DIR" ./wallfacer run -data "$DATA" -addr ":$PORT" -no-browser >/tmp/wf-shots-server.log 2>&1 &
SERVER_PID=$!
trap 'kill "$SERVER_PID" 2>/dev/null || true' EXIT
for _ in $(seq 1 30); do
  curl -sf "$BASE/" >/dev/null 2>&1 && break || sleep 1
done

# Every surface the docs, the landing page, or the README embed. The seed
# gives each one content (tasks in every state, a routine, an artifact, the
# repo's own spec tree), so one run refreshes the whole set.
DOC_SURFACES="board,task-detail,plan,routines,agents,mission,whiteboard,analytics"
SURFACES="$DOC_SURFACES,overview-spec,oversight"
snap() { node "$PW/snap.mjs" --base "$BASE" --out "$OUT" --only "$SURFACES" "$@"; }

echo "==> Capturing light + dark"
rm -rf "$OUT"; mkdir -p "$OUT"
snap >/dev/null
snap --theme dark >/dev/null

echo "==> Distributing to committed locations"
# docs/guide/images: a light + dark pair per surface, downscaled from the 2x
# capture to a doc-friendly width (the markdown renderer derives -dark).
for name in ${DOC_SURFACES//,/ }; do
  for variant in "" "-dark"; do
    sips --resampleWidth 1600 "$OUT/$name$variant.png" --out "docs/guide/images/$name$variant.png" >/dev/null
  done
done

# Landing (frontend/public/static) — light + dark pairs, theme-swapped in
# ProductPage.vue. Source surface differs from the destination name.
cp "$OUT/board.png"              frontend/public/static/overview-kanban.png
cp "$OUT/board-dark.png"         frontend/public/static/overview-kanban-dark.png
cp "$OUT/overview-spec.png"      frontend/public/static/overview-spec.png
cp "$OUT/overview-spec-dark.png" frontend/public/static/overview-spec-dark.png
cp "$OUT/oversight.png"          frontend/public/static/oversight1.png
cp "$OUT/oversight-dark.png"     frontend/public/static/oversight1-dark.png
cp "$OUT/analytics.png"          frontend/public/static/usage.png
cp "$OUT/analytics-dark.png"     frontend/public/static/usage-dark.png

# README (assets) — GitHub renders light, so only the light variant.
cp "$OUT/board.png"         assets/overview-board.png
cp "$OUT/overview-spec.png" assets/overview-spec.png
cp "$OUT/oversight.png"     assets/oversight1.png
cp "$OUT/analytics.png"     assets/usage.png

echo "==> Done. Review with: git status --short docs/guide/images frontend/public/static assets"
