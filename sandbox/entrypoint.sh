#!/bin/bash
set -e

# Fix git "dubious ownership" for mounted volumes
git config --global --add safe.directory '*'

# Ensure claude config file exists (prevents backup-restore loop)
if [ ! -f "$HOME/.claude.json" ]; then
    echo '{}' > "$HOME/.claude.json"
fi

# Pass through all arguments to claude
exec claude --dangerously-skip-permissions "$@"
