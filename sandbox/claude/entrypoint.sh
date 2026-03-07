#!/bin/bash
set -e

# Ensure claude config file exists (prevents backup-restore loop)
if [ ! -f "$HOME/.claude.json" ]; then
    echo '{}' > "$HOME/.claude.json"
fi

# Pass through all arguments to claude
exec claude --dangerously-skip-permissions "$@"
