#!/usr/bin/env bash
set -euo pipefail

# Push current branch once after a sequence of local commits.
#
# Usage:
#   scripts/push-once.sh
#   scripts/push-once.sh origin main

remote="${1:-origin}"
branch="${2:-$(git branch --show-current)}"

if [[ -z "${branch}" ]]; then
  echo "Cannot determine current branch." >&2
  exit 2
fi

ahead="$(git rev-list --count "${remote}/${branch}..${branch}" 2>/dev/null || echo 0)"
if [[ "${ahead}" == "0" ]]; then
  echo "No local commits to push for ${branch}."
  exit 0
fi

git push "${remote}" "${branch}"
