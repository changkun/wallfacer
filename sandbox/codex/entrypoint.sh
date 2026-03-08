#!/bin/bash

# Parse wallfacer-style arguments and translate to codex CLI format.
# Wallfacer passes Claude Code-style flags:
#   -p <prompt> --verbose --output-format <val> [--model <val>] [--resume <val>]
#
# We run Codex in non-interactive mode:
#   codex exec --full-auto [--model <val>] --output-last-message <file> <prompt>
#
# Then wrap the final assistant message in a Claude Code-compatible JSON envelope
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

LAST_MSG_FILE="/tmp/codex-last-message.txt"
STDERR_FILE="/tmp/codex-stderr.txt"
rm -f "$LAST_MSG_FILE" "$STDERR_FILE"

CODEX_ARGS=(exec --full-auto --sandbox workspace-write --skip-git-repo-check --output-last-message "$LAST_MSG_FILE" --color never)
if [ -n "$MODEL" ]; then
    CODEX_ARGS+=(--model "$MODEL")
fi

# Run codex, capturing stdout separately from stderr. The final agent message is
# written to LAST_MSG_FILE; stderr is fallback context for errors.
set +e
STDOUT_OUTPUT=$(codex "${CODEX_ARGS[@]}" "$PROMPT" 2>"$STDERR_FILE")
EXIT_CODE=$?
set -e

IS_ERROR="false"
STOP_REASON="end_turn"

OUTPUT=""
if [ -s "$LAST_MSG_FILE" ]; then
    OUTPUT=$(cat "$LAST_MSG_FILE")
elif [ -n "$STDOUT_OUTPUT" ]; then
    OUTPUT="$STDOUT_OUTPUT"
elif [ -s "$STDERR_FILE" ]; then
    OUTPUT=$(cat "$STDERR_FILE")
fi

# Drop known non-fatal CLI warning lines that can leak into fallback stderr
# output and break downstream JSON parsing expectations.
OUTPUT=$(printf '%s' "$OUTPUT" | sed '/^WARNING: proceeding, even though we could not update PATH:/d')

# Treat runs that produced a concrete final message as success, even if codex
# exits non-zero due non-fatal warnings. Only mark error when no useful output
# was produced and the command failed.
if [ "$EXIT_CODE" -ne 0 ] && [ -z "$OUTPUT" ]; then
    IS_ERROR="true"
fi

# Emit a Claude Code-compatible JSON result so wallfacer can parse it.
ESCAPED_OUTPUT=$(printf '%s' "$OUTPUT" | jq -Rs .)
printf '{"result":%s,"session_id":"","stop_reason":"%s","is_error":%s,"total_cost_usd":0,"usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}\n' \
    "$ESCAPED_OUTPUT" "$STOP_REASON" "$IS_ERROR"
