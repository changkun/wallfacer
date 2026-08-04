#!/usr/bin/env bash
# Move the spec workflow skills between their upstream home and the copy
# committed here.
#
# Upstream is latere-ai/claude-plugins, where the skills are plugin skills:
# unprefixed directory names invoked as /spec:create. The committed copy is a
# project skill directory, where those names would be far too generic, so it
# carries the wf-spec- prefix. The rename is the only difference, and it is
# deterministic in both directions:
#
#   plugins/spec/skills/create/SKILL.md   <->  .claude/skills/wf-spec-create/skill.md
#   name: create                          <->  name: wf-spec-create
#   /spec:refine                          <->  /wf-spec-refine
#
# Modes:
#   pull    upstream -> .claude/skills/         (adopt upstream changes)
#   push    .claude/skills/ -> upstream         (promote local edits)
#   check   diff both ways; non-zero on drift   (CI gate)
#
# Upstream resolves to ../claude-plugins when that clone exists, else a shallow
# clone into a temp directory. Set SKILLS_UPSTREAM to override.
set -euo pipefail

mode=${1:-check}
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
vendored="$repo_root/.claude/skills"

UPSTREAM_REPO=${UPSTREAM_REPO:-https://github.com/latere-ai/claude-plugins.git}
cleanup=""
resolve_upstream() {
	if [[ -n ${SKILLS_UPSTREAM:-} ]]; then
		echo "$SKILLS_UPSTREAM"
		return
	fi
	local sibling="$repo_root/../claude-plugins"
	if [[ -d $sibling/.git ]]; then
		(cd "$sibling" && pwd)
		return
	fi
	# No sibling clone: fetch a throwaway one. Pushing into it would be lost,
	# so refuse that mode rather than silently discard the user's edits.
	if [[ $mode == push ]]; then
		echo "push needs a writable clone at ../claude-plugins (or SKILLS_UPSTREAM)" >&2
		exit 2
	fi
	cleanup=$(mktemp -d)
	git clone --quiet --depth 1 "$UPSTREAM_REPO" "$cleanup" >&2
	echo "$cleanup"
}
upstream=$(resolve_upstream)
trap '[[ -n $cleanup ]] && rm -rf "$cleanup"' EXIT

skills_dir="$upstream/plugins/spec/skills"
[[ -d $skills_dir ]] || { echo "no skills at $skills_dir" >&2; exit 2; }

# to_vendored/to_upstream rewrite one file's body between the two conventions.
to_vendored() {
	sed -E -e 's/^name: ([a-z-]+)$/name: wf-spec-\1/' \
	       -e 's|/spec:([a-z-]+)|/wf-spec-\1|g' \
	       -e 's/`spec:([a-z-]+)`/`wf-spec-\1`/g' \
	       -e 's|`/spec:\*`|`/wf-spec-*`|g'
}
to_upstream() {
	sed -E -e 's/^name: wf-spec-([a-z-]+)$/name: \1/' \
	       -e 's|/wf-spec-([a-z-]+)|/spec:\1|g' \
	       -e 's/`wf-spec-([a-z-]+)`/`spec:\1`/g' \
	       -e 's|`/wf-spec-\*`|`/spec:*`|g'
}

# Skills the vendored copy deliberately omits: housekeeping only applies to
# flat-numbered spec trees, and this repo uses track directories.
skip_vendored() { [[ $1 == housekeeping ]]; }

drift=0
report() { echo "$1"; drift=1; }

for src in "$skills_dir"/*/SKILL.md; do
	name=$(basename "$(dirname "$src")")
	skip_vendored "$name" && continue
	dst="$vendored/wf-spec-$name/skill.md"
	case $mode in
	pull)
		mkdir -p "$(dirname "$dst")"
		to_vendored <"$src" >"$dst"
		;;
	check)
		if [[ ! -f $dst ]]; then
			report "missing vendored skill: wf-spec-$name"
		elif ! diff -q <(to_vendored <"$src") "$dst" >/dev/null; then
			report "drift: .claude/skills/wf-spec-$name/skill.md differs from upstream"
		fi
		;;
	esac
done

for dst in "$vendored"/wf-spec-*/skill.md; do
	name=$(basename "$(dirname "$dst")")
	name=${name#wf-spec-}
	src="$skills_dir/$name/SKILL.md"
	case $mode in
	push)
		mkdir -p "$(dirname "$src")"
		to_upstream <"$dst" >"$src"
		;;
	check)
		[[ -f $src ]] || report "vendored skill has no upstream: wf-spec-$name"
		;;
	esac
done

case $mode in
pull) echo "pulled $skills_dir -> $vendored" ;;
push) echo "pushed $vendored -> $skills_dir (commit and push in $upstream)" ;;
check)
	if ((drift)); then
		echo "run 'make skills-pull' to adopt upstream, or 'make skills-push' to promote local edits" >&2
		exit 1
	fi
	echo "skills match upstream"
	;;
*) echo "usage: skills.sh [pull|push|check]" >&2; exit 2 ;;
esac
