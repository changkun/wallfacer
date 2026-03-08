#!/usr/bin/env bash
set -euo pipefail

# Enforce repo commit style:
#   <scope>[,<scope2>...]: <summary>
# and require a non-empty body description.
#
# Usage:
#   scripts/commit-seq.sh "internal/runner: fix fallback behavior" "Explain what changed and why."

msg="${1:-}"
desc="${2:-}"

if [[ -z "${msg}" || -z "${desc}" ]]; then
  echo "Usage: scripts/commit-seq.sh \"<scope>: <summary>\" \"<description body>\"" >&2
  exit 2
fi

style_re='^[a-z0-9_./-]+(,[a-z0-9_./-]+)*: .+$'
if [[ ! "${msg}" =~ ${style_re} ]]; then
  echo "Invalid commit subject style." >&2
  echo "Expected: <scope>[,<scope2>...]: <summary>" >&2
  echo "Example: internal/handler,ui/js: gate codex usage on key and passing sandbox test" >&2
  exit 2
fi

trim_desc="$(echo "${desc}" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')"
if [[ -z "${trim_desc}" ]]; then
  echo "Commit description body must be non-empty." >&2
  exit 2
fi

if [[ -z "$(git diff --cached --name-only)" ]]; then
  echo "No staged changes. Stage files first (git add ...)." >&2
  exit 2
fi

git commit -m "${msg}" -m "${trim_desc}"
echo "Committed. Continue with next logical change, then push once at the end."
