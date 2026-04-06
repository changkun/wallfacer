#!/bin/bash

# Parse wallfacer-style arguments and translate to codex CLI format.
# Wallfacer passes Claude Code-style flags:
#   -p <prompt> --verbose --output-format <val> [--model <val>] [--resume <val>]
#
# We run Codex in non-interactive mode:
#   codex exec --full-auto [--model <val>] --output-last-message <file> <prompt>
#
# Then wrap the final assistant message in a Claude Code-compatible JSON envelope
# so wallfacer can parse the result correctly while preserving usage metadata
# from the Codex JSON event stream.

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
STREAM_FILE="/tmp/codex-stream.jsonl"
rm -f "$LAST_MSG_FILE" "$STDERR_FILE" "$STREAM_FILE"

CODEX_ARGS=(exec --full-auto --sandbox workspace-write --skip-git-repo-check --json --output-last-message "$LAST_MSG_FILE" --color never)
if [ "${WALLFACER_SANDBOX_FAST:-true}" != "false" ]; then
    CODEX_ARGS+=(--config model_reasoning_effort=\"low\")
fi
if [ -n "$MODEL" ]; then
    CODEX_ARGS+=(--model "$MODEL")
fi

# Run codex in streaming JSON mode. Stdout is both forwarded (for live logs)
# and captured to STREAM_FILE for fallback parsing; stderr is captured separately.
set +e
codex "${CODEX_ARGS[@]}" "$PROMPT" 2>"$STDERR_FILE" | tee "$STREAM_FILE"
EXIT_CODE=${PIPESTATUS[0]}
set -e

IS_ERROR="false"
STOP_REASON="end_turn"
SESSION_ID=""
TOTAL_COST_USD="0"
USAGE_JSON='{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}'

OUTPUT=""
if [ -s "$LAST_MSG_FILE" ]; then
    OUTPUT=$(cat "$LAST_MSG_FILE")
elif [ -s "$STREAM_FILE" ]; then
    OUTPUT=$(tail -n 1 "$STREAM_FILE")
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

# Recover usage/session metadata from the streamed Codex JSON events. Codex
# emits token usage on turn.completed using cached_input_tokens rather than the
# Claude-compatible cache_read_input_tokens field name expected by wallfacer.
if [ -s "$STREAM_FILE" ]; then
    STREAM_META=$(jq -Rsc '
        [splits("\n") | select(length > 0) | (fromjson? // empty)] as $events |
        ($events | map(select(.type == "turn.completed" and (.usage != null))) | last // {}) as $turn |
        ($events | map(select((.session_id // "") != "")) | last // {}) as $session |
        ($events | map(select((.stop_reason // "") != "")) | last // {}) as $stop |
        ($events | map(select(.total_cost_usd? != null)) | last // {}) as $cost |
        {
            session_id: ($session.session_id // ""),
            stop_reason: ($stop.stop_reason // "end_turn"),
            total_cost_usd: ($cost.total_cost_usd // 0),
            usage: {
                input_tokens: ($turn.usage.input_tokens // 0),
                output_tokens: ($turn.usage.output_tokens // 0),
                cache_read_input_tokens: ($turn.usage.cache_read_input_tokens // $turn.usage.cached_input_tokens // 0),
                cache_creation_input_tokens: ($turn.usage.cache_creation_input_tokens // 0)
            }
        }
    ' "$STREAM_FILE" 2>/dev/null || true)

    if [ -n "$STREAM_META" ]; then
        SESSION_ID=$(printf '%s' "$STREAM_META" | jq -r '.session_id // ""')
        STOP_REASON=$(printf '%s' "$STREAM_META" | jq -r '.stop_reason // "end_turn"')
        TOTAL_COST_USD=$(printf '%s' "$STREAM_META" | jq -r '.total_cost_usd // 0')
        USAGE_JSON=$(printf '%s' "$STREAM_META" | jq -c '.usage')
    fi
fi

# Emit a Claude Code-compatible JSON result so wallfacer can parse it.
ESCAPED_OUTPUT=$(printf '%s' "$OUTPUT" | jq -Rs .)
ESCAPED_SESSION_ID=$(printf '%s' "$SESSION_ID" | jq -Rs .)
ESCAPED_STOP_REASON=$(printf '%s' "$STOP_REASON" | jq -Rs .)
printf '{"result":%s,"session_id":%s,"stop_reason":%s,"is_error":%s,"total_cost_usd":%s,"usage":%s}\n' \
    "$ESCAPED_OUTPUT" "$ESCAPED_SESSION_ID" "$ESCAPED_STOP_REASON" "$IS_ERROR" "$TOTAL_COST_USD" "$USAGE_JSON"
