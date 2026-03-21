#!/usr/bin/env bash
# Generate release notes for a new version by analyzing the diff since
# the last tag. Pipes a prompt through claude and writes the result to
# docs/releases/<version>.md.
#
# Usage:
#   ./scripts/release-notes.sh v0.0.6
set -euo pipefail

VERSION="${1:-}"
PREV_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "")

if [[ -z "$PREV_TAG" ]]; then
  echo "Error: no previous tag found" >&2
  exit 1
fi

if [[ -z "$VERSION" ]]; then
  echo "Usage: $0 <version>" >&2
  echo "Previous tag: $PREV_TAG" >&2
  exit 1
fi

REPO_ROOT=$(git rev-parse --show-toplevel)
OUTFILE="${REPO_ROOT}/docs/releases/${VERSION}.md"

PREV_DATE=$(git log -1 --format=%cs "$PREV_TAG")
TODAY=$(date +%Y-%m-%d)
COMMIT_COUNT=$(git rev-list "${PREV_TAG}..HEAD" --count)
DIFFSTAT=$(git diff --stat "${PREV_TAG}..HEAD" | tail -1)

echo "Generating release notes for $VERSION (${PREV_TAG}..HEAD)..." >&2
echo "  Previous: $PREV_TAG ($PREV_DATE)" >&2
echo "  Commits:  $COMMIT_COUNT" >&2
echo "  Diff:     $DIFFSTAT" >&2
echo "  Output:   $OUTFILE" >&2
echo "" >&2

# Collect raw data for the LLM prompt
COMMIT_LOG=$(git log --oneline "${PREV_TAG}..HEAD")
DIFF_SUMMARY=$(git diff --stat "${PREV_TAG}..HEAD")
FILE_CHANGES=$(git diff --name-only "${PREV_TAG}..HEAD" | sort)

# Read previous release notes for style reference
PREV_NOTES=""
PREV_NOTES_FILE="${REPO_ROOT}/docs/releases/${PREV_TAG}.md"
if [[ -f "$PREV_NOTES_FILE" ]]; then
  PREV_NOTES=$(cat "$PREV_NOTES_FILE")
fi

PROMPT=$(cat <<ENDPROMPT
You are writing release notes for wallfacer $VERSION.

## Context

wallfacer is a task-board orchestration system for autonomous coding agents.
It provides a web UI where tasks are created as cards, run in isolated sandbox
containers via Claude Code or OpenAI Codex, and results are reviewed.

## Release metadata

- Release date: $TODAY
- Previous release: $PREV_TAG ($PREV_DATE)
- Range: ${PREV_TAG}..${VERSION}
- Delta: $COMMIT_COUNT commits, $DIFFSTAT

## Commits since $PREV_TAG

$COMMIT_LOG

## Files changed

$DIFF_SUMMARY

## Changed file paths

$FILE_CHANGES

## Previous release notes (for style reference)

$PREV_NOTES

## Instructions

1. Start with a punchy subtitle (## 🚀 v$VERSION — The "..." Release)
2. Include the release metadata block
3. Write a 1-2 sentence hook that captures why this release matters
4. Group changes into 3-6 themed sections with emoji headers
5. Each section: catchy title, 3-5 bullet points explaining what changed and why it matters to users
6. End with an operator notes section if there are breaking changes or migration steps
7. Close with a bold summary line (commit count, lines changed, tagline)
8. Be exciting, energetic, and inspiring — but grounded in what actually shipped
9. Focus on user-facing impact, not internal refactoring
10. Use **bold** for emphasis on key features

Output ONLY the release notes markdown, nothing else.
ENDPROMPT
)

# Check for claude CLI
if ! command -v claude &>/dev/null; then
  echo "Error: claude CLI not found. Install it or pipe the prompt manually:" >&2
  echo "  ./scripts/release-notes.sh $VERSION --prompt-only > prompt.txt" >&2
  echo "  Then feed prompt.txt to your preferred LLM and save output to $OUTFILE" >&2
  exit 1
fi

echo "# ${VERSION}" > "$OUTFILE"
echo "" >> "$OUTFILE"
echo "$PROMPT" | claude --print >> "$OUTFILE"

echo "" >&2
echo "Release notes written to $OUTFILE" >&2
echo "Review and edit before running: make release RELEASE_VERSION=$VERSION" >&2
