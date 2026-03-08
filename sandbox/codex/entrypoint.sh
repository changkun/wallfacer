#!/bin/bash

# Parse wallfacer-style arguments and translate to codex CLI format.
# Wallfacer passes Claude Code flags: -p <prompt> --verbose --output-format <val>
#   [--model <val>] [--resume <val>]
# Codex CLI expects: [--model <val>] <prompt>
#
# Model priority:
#   1. --model CLI arg (wallfacer per-task override or CLAUDE_DEFAULT_MODEL)
#   2. CODEX_DEFAULT_MODEL environment variable
#
# After codex runs, wrap the output in a Claude Code-compatible JSON envelope
# so wallfacer can parse the result correctly.

PROMPT=""
MODEL="${CODEX_DEFAULT_MODEL:-}"

while [[ $# -gt 0 ]]; do
    case $1 in
        -p)
            PROMPT="$2"
            shift 2
            ;;
        --model|-m)
            MODEL="$2"
            shift 2
            ;;
        --output-format|--resume)
            shift 2  # skip flag and its value
            ;;
        --verbose)
            shift    # skip flag only
            ;;
        *)
            shift
            ;;
    esac
done

CODEX_ARGS=(--approval-mode full-auto)
if [ -n "$MODEL" ]; then
    CODEX_ARGS+=(--model "$MODEL")
fi

# Run codex, capturing combined stdout+stderr.
set +e
OUTPUT=$(codex "${CODEX_ARGS[@]}" "$PROMPT" 2>&1)
EXIT_CODE=$?
set -e

IS_ERROR="false"
STOP_REASON="end_turn"
if [ "$EXIT_CODE" -ne 0 ]; then
    IS_ERROR="true"
fi

# Emit a Claude Code-compatible JSON result so wallfacer can parse it.
ESCAPED_OUTPUT=$(printf '%s' "$OUTPUT" | jq -Rs .)
printf '{"result":%s,"session_id":"","stop_reason":"%s","is_error":%s,"total_cost_usd":0,"usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}\n' \
    "$ESCAPED_OUTPUT" "$STOP_REASON" "$IS_ERROR"
