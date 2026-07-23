#!/usr/bin/env bash
# Guardrail against byte-index truncation of strings.
#
# Slicing a Go string by byte offset can cut inside a multi-byte UTF-8
# sequence. The partial sequence survives in memory but is replaced with U+FFFD
# the moment it is JSON-marshalled, written to a log, or persisted, so the
# corruption surfaces far from its cause. sanitize.Truncate (and the runner's
# `truncate` alias) slice by rune instead and are the only sanctioned way to
# shorten display text.
#
# Two syntactic shapes are flagged, both specific to display truncation and
# neither matching the byte-safe slices in the tree (uuid/hex prefixes,
# fixed-width status codes, `x[:0]` resets):
#
#   1. a slice concatenated with an ellipsis:  s[:80] + "..."
#   2. a slice guarded by a byte-length test:  if len(s) > 80 { s = s[:80] }
#
# Lines that cannot split a rune (element slices, ASCII-by-construction values)
# opt out with a trailing `// utf8-safe: <reason>` comment. A deliberate byte
# budget opts out by sanitizing the tail with strings.ToValidUTF8.

set -euo pipefail

cd "$(dirname "$0")/.."

files=$(find . -name '*.go' -not -name '*_test.go' -not -path './frontend/*' -not -path './.git/*')

hits=$(
  # Shape 1: byte slice concatenated with an ellipsis.
  grep -nE '\[:[^]]+\][[:space:]]*\+[[:space:]]*"(\.\.\.|…)"' $files || true
  # Shape 2: byte slice whose bound is the same as an enclosing len() guard.
  awk '
    /^[[:space:]]*(if|} else if)[[:space:]]+len\(/ && match($0, />=?[[:space:]]*[A-Za-z0-9_.]+[[:space:]]*\{/) {
      bound = substr($0, RSTART, RLENGTH)
      sub(/^>=?[[:space:]]*/, "", bound)
      sub(/[[:space:]]*\{$/, "", bound)
      guard = bound
      window = 4
      next
    }
    window > 0 {
      window--
      if (index($0, "[:" guard "]") > 0) {
        print FILENAME ":" FNR ":" $0
      }
    }
  ' $files || true
)

# Drop explicit opt-outs and byte budgets that sanitize their tail.
hits=$(printf '%s' "$hits" | grep -vE '// utf8-safe|ToValidUTF8' | sort -u || true)

if [ -n "$hits" ]; then
  echo "byte-index truncation of a string; use sanitize.Truncate (rune-safe):"
  echo "$hits"
  exit 1
fi
