---
name: wf-spec-housekeeping
description: Repair the numbering of a specs/ directory: give name-only specs a stable NNN id, retire terminal specs into .archive/ keeping their number, fix the cross-references the moves break, and optionally rebuild the index in number order. Moves files and commits. Operates on one directory at a time, so it works whether specs sit flat under specs/ or inside a track.
argument-hint: [--number-only | --index-only] [target-dir]
allowed-tools: Read, Grep, Glob, Edit, Write, Agent, Bash(ls *), Bash(git mv *), Bash(git add *), Bash(git restore *), Bash(git commit *), Bash(git status *), Bash(git log *), Bash(git push *), Bash(grep *), Bash(sed *), Bash(awk *), Bash(perl *), Bash(cat *), Bash(wc *), Bash(sort *), Bash(uniq *), Bash(cut *), Bash(basename *)
---

# Specs housekeeping

Bring one specs directory back to a single invariant: **every spec in it is
`NNN-name.md` in that directory's own number space; active specs stay in place,
terminal specs retire under `.archive/` but keep their number so `depends_on:`
frontmatter still resolves.** Then rebuild the index for that directory in
number order.

The scope is a *directory*, not a repo. Numbering is a per-directory reading
order that composes with track grouping: `specs/local/003-live-serve.md` is
both grouped and numbered, and `specs/local/003-…` and `specs/cloud/003-…` are
separate number spaces that do not collide. Whatever moves here, the dependency
graph is unaffected — `depends_on` paths are repo-root-relative and form one
DAG across the whole tree, so keeping numbers stable across an archive move is
what keeps that graph resolving.

`$ARGUMENTS` may contain a mode flag and/or a target directory:
- default (no flag): do both the numbering pass and the README rebuild.
- `--number-only`: number + archive + fix references; skip the README rebuild.
- `--index-only`: rebuild the README index only; assume numbering is already clean.
- a path: operate on that directory — either another repo's `specs/`, or a
  single track inside one (`specs/local/`).

This skill mutates files, renames with history, and commits. Work on `main` only
if that is the repo's convention (check `git log`); otherwise branch first.

## Step 0: Fix the scope

Decide which single directory to work on. A path in `$ARGUMENTS` names it
directly; otherwise use `specs/` itself. Glob that directory's own `*.md`
(not recursively — sub-directories are separate scopes, and child specs under a
`<parent>/` folder belong to their parent, not to this number space).

Then read the intent already in that directory:
- Some or all specs already carry an `NNN-` prefix → it has adopted numbering;
  proceed to make it consistent.
- No spec carries one → adopting numbering is a new convention, not a repair.
  Say so and confirm with the user before renaming anything.

A directory full of track sub-directories and no specs of its own has nothing
to number. Say which sub-directories are candidates and let the user pick one,
rather than recursing on your own.

Look at a sibling repo's `specs/README.md` (or lux's, if reachable at
`../lux/specs/README.md`) as the reference shape for the index: a status-legend
line plus one `| # | Spec | Status |` table ordered by number, active rows
linking to the root and archived rows into `.archive/`.

## Step 1: Inventory

Extract `(number, location, filename, status, title)` for every spec. Numbers
come from the `NNN` filename prefix; name-only files have none yet.

```bash
cd <repo>/specs
meta() { local loc="$1"; shift; for f in "$@"; do
  n=$(basename "$f" | grep -oE '^[0-9]+')
  t=$(grep -m1 '^title:' "$f" | sed 's/^title: *//; s/^"//; s/"$//')
  s=$(grep -m1 '^status:' "$f" | sed 's/^status: *//; s/ *$//')
  printf '%s\t%s\t%s\t%s\t%s\n' "${n:-NONE}" "$loc" "$(basename "$f")" "${s:-none}" "$t"
done; }
meta root    $(ls [0-9]*.md 2>/dev/null)      # numbered active
meta root    $(ls *.md 2>/dev/null | grep -vE '^[0-9]|^README')  # NAME-ONLY: the drift
meta archive $(ls .archive/[0-9]*.md 2>/dev/null)
```

The name-only list is the work. For each, read its frontmatter `status`.

## Step 2: Classify — terminal vs live

Status vocab varies by repo; map onto two buckets:

- **Terminal** (archive it): `complete`, `shipped`, `implemented`, `superseded`,
  `abandoned`, `archived`, `stale`, `deferred`. Also treat a `draft`/`drafted`
  spec that carries an **Outcome** section (shipped-but-status-stale) as
  terminal, and flip its status to `implemented` when you move it.
- **Live** (keep at the root, just give it a number if it lacks one): `vague`,
  `drafted`/`draft` with no Outcome, `validated`, `testing`, `in-progress`.

When a name-only spec's status is ambiguous, read the body tail for an
`## Outcome` / "Done"/"shipped" signal before deciding. If still unclear, ask.

## Step 3: Assign numbers

Numbers are **stable IDs**; never renumber an existing spec. Find the highest
number in use across **both** the root and `.archive/`, and append from
`max+1` in created-date order (read each `created:` field). Created-order is
a good default because it keeps a superseding spec after the specs it
supersedes; contiguous same-family numbering is an equally fine alternative.

## Step 4: Cross-reference grep BEFORE renaming (the step that blocks)

A name-only spec is often a **cross-repo companion** — the same bare filename
exists in sibling repos, and its own frontmatter `depends_on`/`affects` may
point across `../`. Renaming can strand references. Grep the whole workspace
for every basename first:

```bash
cd <workspace-root>   # the dir that holds all sibling repos, e.g. ../
for b in <basename1> <basename2> ...; do
  echo "=== $b.md ==="
  grep -rIn --exclude-dir=node_modules --exclude-dir=.git "$b\.md" .
done
```

Triage the hits:
- **Same-repo hard links** — this repo's own frontmatter (`supersedes:`,
  `superseded_by:`, `depends_on:`), README rows, and internal doc links. **You
  must fix every one of these** in the rename commit.
- **Sibling-repo index/prose listings** (another repo's README pointing at
  `thisrepo/specs/name.md`). These are usually already stale ecosystem-wide and
  are that repo's housekeeping, not yours. Do **not** edit sibling repos; note
  them for the user instead. (If the ecosystem convention is that every repo
  numbers its own copy, your rename matches it — say so.)

## Step 5: Move with `git mv` and fix same-repo references

```bash
git mv name.md .archive/NNN-name.md   # history follows; one per file
```

Then, in the same working set, fix every same-repo reference found in Step 4:
- `superseded_by:` / `supersedes:` / `depends_on:` frontmatter paths that
  pointed at the old location (use the repo's path convention — usually
  repo-root-relative `specs/.archive/NNN-name.md`).
- README table rows and internal `docs/**` markdown links.
- Flip any shipped-but-`draft` status to `implemented`.

Leave live-but-newly-numbered specs at the root: `git mv name.md NNN-name.md`.

## Step 6: Verify the moves (do this before committing)

```bash
# a) every same-repo depends_on / supersede target still resolves
for f in specs/[0-9]*.md specs/.archive/[0-9]*.md; do
  awk '/^(depends_on|supersedes):/{c=1;next} /^[a-z_]+:/{c=0}
       c&&/^ *- /{gsub(/^ *- */,"");gsub(/"/,"");print}
       /^superseded_by:/{v=$2;gsub(/"/,"",v);print v}' "$f" \
  | while read p; do case "$p" in specs/*) [ -f "$p" ] || echo "BROKEN $f -> $p";; esac; done
done
```
Any `BROKEN` line must be fixed before you commit. Optionally run the
`wf-spec-validate` skill for the full document-model check.

## Step 7: Commit the numbering pass (atomic)

One commit for renames + cross-reference fixes, README excluded:

```bash
git add -A && git restore --staged specs/README.md
git commit -m "specs: number the N name-only specs into .archive/ (NNN-MMM)"
```

Follow the repo's commit conventions (check `git log`): small scoped diffs, no
Co-Authored-By trailer unless the repo uses one, no em dashes in the message if
the repo avoids them.

## Step 8: Rebuild README as a clean index (skip if `--number-only`)

Generate the table **mechanically** from the inventory; do not hand-type ~100
rows or invent descriptions (use each spec's `title` as the topic).

```bash
cd <repo>/specs
{ meta root $(ls [0-9]*.md); meta archive $(ls .archive/[0-9]*.md); } \
 | sort -t$'\t' -k1,1n \
 | awk -F'\t' '{
     n=$1; loc=$2; file=$3; st=$4; title=$5
     link=(loc=="root")?file:".archive/" file
     if(loc=="root"){
       d=(st=="drafted"||st=="draft")?"drafted":
         (st=="in-progress")?"in progress":
         (st=="implemented")?"✅ implemented":
         (st=="complete")?"✅ complete":
         (st=="superseded")?"superseded": st
     } else {
       d=(st=="abandoned")?"📦 archived (abandoned)":
         (st=="superseded")?"📦 archived (superseded)":
         (st=="archived")?"📦 archived":"📦 archived (shipped)"
     }
     printf "| [%s](%s) | %s | %s |\n", n, link, title, d
   }' > /tmp/spec-index-table.md
```

Assemble the README from three parts (write head + foot with the Write tool,
then `cat head table foot > README.md`) so you never paste the long table by
hand:

- **Head** — the repo's existing intro + a folder-layout paragraph (drop any
  now-false "unnumbered drafts sit at the root" sentence) + a status legend
  matching the badges above + the `## Specs` heading and table header
  (`| # | Spec | Status |`).
- **Table** — the generated `/tmp/spec-index-table.md`.
- **Foot** — the **institutional-memory** sections a flat table cannot encode.
  Preserve, do not delete: in-progress state, deferral triggers ("un-defer
  when…"), pending external/tunable decisions, locked decisions, and
  closed-scope ("do not re-litigate") lists. Fold any old *Shipped tracks* /
  *Native backend* / per-tier tables into the one index — they are now
  redundant. Remove any pointer to a deleted `ARCHIVED.md`.

Non-numbered archive files (`*-README.md`, spikes, reconciled drafts) stay
unlisted in the table; mention them once in the folder-layout paragraph.

## Step 9: Verify the README, then commit

```bash
# every markdown link in the README resolves (skip http and ../ cross-repo)
grep -oE '\]\(([^)]+\.md)\)' specs/README.md | sed -E 's/\]\(//; s/\)$//' | sort -u \
 | while read p; do case "$p" in http*|../*) ;; *) [ -f "specs/$p" ] || echo "MISSING $p";; esac; done
```
Fix any `MISSING`, then commit the README as the second atomic diff:

```bash
git add specs/README.md
git commit -m "specs: rebuild README as a single NNN-ordered index"
```

Push if the repo pushes to main directly (check convention).

## Report

Tell the user: how many name-only specs were numbered and their new
`NNN-name.md` targets; which were kept live at the root vs archived; every
same-repo reference fixed; any **sibling-repo** listings left stale on purpose
(with paths, so they can fix those repos); and whether the README was rebuilt.

## Gotchas (learned the hard way)

- **The `.archive` grep undercounts the max number.** Active specs at the root
  usually hold the highest numbers. Compute `max` over root **and** `.archive/`.
- **Cross-repo companions look local but aren't.** `auth-unification-migration`,
  `local-build-deploy`, and similar share a bare filename across every product
  repo. Grep `../` before renaming; expect sibling READMEs to reference the old
  name and leave them alone.
- **A `draft` status can be a lie.** A spec with an `## Outcome` section shipped;
  treat it as terminal and correct the status on the way to `.archive/`.
- **Never renumber.** Gaps in the sequence are fine and expected (folded/reverted
  specs). Only ever append at `max+1`.
- **Don't delete institutional memory.** "Clean index" means one ordered table
  *plus* the decision-record prose, not a table that erased why things were
  deferred or ruled out.
