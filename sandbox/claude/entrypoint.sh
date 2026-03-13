#!/bin/bash
set -e

# Ensure claude config file exists (prevents backup-restore loop)
if [ ! -f "$HOME/.claude.json" ]; then
    echo '{}' > "$HOME/.claude.json"
fi

CLAUDE_ARGS=(--dangerously-skip-permissions)
if [ "${WALLFACER_SANDBOX_FAST:-true}" != "false" ]; then
    CLAUDE_ARGS+=(--append-system-prompt "/fast")
fi

exec claude "${CLAUDE_ARGS[@]}" "$@"
