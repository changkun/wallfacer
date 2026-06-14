#!/bin/bash
set -e

# Ensure claude config file exists (prevents backup-restore loop)
if [ ! -f "$HOME/.claude.json" ]; then
    echo '{}' > "$HOME/.claude.json"
fi

# Initialize RTK hooks in the mounted config volume
if command -v rtk &>/dev/null; then
    rtk init --global 2>/dev/null || true
fi

exec claude --dangerously-skip-permissions "$@"
