---
title: Spec Workflow as an Installable Plugin
status: complete
depends_on: []
affects:
  - .claude/skills/
  - internal/agentsession/commands.go
  - internal/agentsession/commands_templates/
  - Makefile
  - .github/workflows/
effort: medium
created: 2026-08-04
updated: 2026-08-04
author: changkun
dispatched_task_id: null
---

# Spec Workflow as an Installable Plugin

Publish the `wf-spec-*` lifecycle skills as a Claude Code plugin under
`github.com/latere-ai`, so the spec-driven workflow can be installed into any
repository instead of being copied by hand. Wallfacer keeps a vendored copy for
harness reach, with a check that fails when it drifts from upstream.

## Why

The workflow currently exists in three places that drift independently:

| Copy | Location | Reach |
| --- | --- | --- |
| Global | `~/.claude/skills/wf-spec-*/` | Interactive sessions on one machine |
| Vendored | `.claude/skills/wf-spec-*/` | Committed; auto-discovered by `claude -p`, so dispatched harness tasks see it |
| Product | `internal/agentsession/commands_templates/*.tmpl` | The in-product plan-mode chat |

Nothing binds them. An audit on 2026-08-04 found the global copy generalized to
a second specs layout while the vendored copy stayed behind; the vendored copy
naming a track set (`foundations`) that had been fully archived; two skills
pointing at a document-model spec that moved into `.archive/`; and the product
templates describing a five-state lifecycle against the seven states in
`internal/spec/lifecycle.go`. The template and documentation defects are already
fixed with guard tests (`8e365ac8`, `291298ef`, `0c03b4c5`, `9771fd51`); this
spec addresses the distribution problem that produced them.

## Scope

### 1. Marketplace repository

`github.com/latere-ai/claude-plugins` is a public marketplace hosting one or
more plugins. The spec workflow ships as the first, named `spec`.

```
claude-plugins/
  .claude-plugin/marketplace.json     # name, owner, plugins[] with source paths
  plugins/spec/
    .claude-plugin/plugin.json        # name, version, description, author
    skills/<name>/SKILL.md            # one directory per skill
    README.md
    LICENSE
  scripts/validate.py                 # manifests vs skill frontmatter, in CI
```

Installation is two steps:

```
/plugin marketplace add latere-ai/claude-plugins
/plugin install spec@latere-ai
```

### 2. Command naming

Plugin skills are namespaced by their plugin, so `wf-spec-create` becomes
`spec:create`. Drop the `wf-spec-` prefix from every skill directory; the
namespace carries it.

Two skills are renamed on the way out, because their names collided with
meanings a reader already has:

| Was | Now | Why |
| --- | --- | --- |
| `diff` | `drift` | The skill does not show a diff; it classifies how far an implementation diverged from its spec |
| `status` | `report` | `status` is the frontmatter field, and read as "this spec's status" when the skill surveys the whole tree |

Descriptions lead with what separates a skill from its neighbours rather than
with mechanics, since the description is all a model sees when routing. The
after-implementation trio needed it most: `review-impl` is read-only and returns
a verdict, `drift` writes that verdict onto the spec, `wrapup` closes the spec
out.

### 3. Server coupling

Eleven of the fourteen skills are pure markdown, git, and frontmatter operations
and port unchanged. Three reach for wallfacer's HTTP API:

| Skill | Endpoint |
| --- | --- |
| `dispatch` | `POST /api/specs/transition`, `POST /api/tasks` |
| `drive` | `POST /api/specs/transition`, `GET /api/tasks/{id}` |
| `drift` | `GET /api/tasks/{id}/diff` |

All three already document a server-unreachable fallback. Invert the framing so
the file-only path is the documented default and the transition API is an
optional accelerator described under a "when a wallfacer server is running"
heading. No skill may assume the API is reachable.

### 4. Repository-specific references

Every remaining wallfacer path (`docs/internals/plan-mode.md`,
`internal/spec/lifecycle.go`, `internal/handler/specs_dispatch.go`) becomes a
described concept plus a "find it in this repo" instruction. The skills already
read the track set off disk rather than naming it; extend the same rule to the
document model, the status vocabulary, and the transition action list.

### 5. Vendoring and the drift check

Wallfacer keeps `.claude/skills/` committed. The Claude harness runs `claude -p`
in containers where no user-level plugin is installed, so dropping the committed
copy would strip dispatched tasks of the workflow. Declaring the marketplace in
project settings does not help: a `.claude/settings.json` carrying
`extraKnownMarketplaces` + `enabledPlugins` was tested against `claude -p` and
the marketplace was never fetched, with the plugin's skills absent from the
session. Upstream is canonical; the vendored copy is a mirror.

The two conventions differ only in naming — unprefixed plugin skills invoked as
`/spec:create` against prefixed project skills invoked as `/wf-spec-create` —
and the rewrite is deterministic in both directions, so edits are legal in
either place:

- `make skills-pull` adopts upstream here, pruning vendored skills upstream no
  longer carries.
- `make skills-push` promotes edits made here into `../claude-plugins`.
- `make skills-check` diffs both directions and exits non-zero on drift. Wired
  into CI, where it shallow-clones upstream when no sibling checkout exists.

All fourteen skills are mirrored, `housekeeping` included: it repairs the
numbering of one directory, so it applies to a wallfacer track that adopts
`NNN-` prefixes just as it does to a flat `specs/`.

Decided 2026-08-04: the mirror stays. Installing from a pinned plugin version at
container build time is not revisited unless Claude Code gains headless plugin
installation.

### 6. Product templates stay a separate surface

The twelve `commands_templates/*.tmpl` prompts and the fourteen skills overlap
but do not match: no template for `drive`, `implement`, or `housekeeping`; no
skill for `summarize`; and `/status` means "set this spec's status" in the
product against "report across all specs" in the skill.

This spec does **not** converge them. The templates stay a deliberately thinner
surface for in-product chat, where the focused spec is already known and the
server is always reachable. The `status` collision is resolved from the plugin
side by the rename in section 2, so nothing in the product had to move.

## Out of scope

- Publishing any other skill (`prose-style`, `deep-tech-book`, `check-docs`) as
  a plugin. Decided 2026-08-04: the marketplace carries the spec workflow only.
- Changing the lifecycle state machine or the transition API.

## Tests

- `make skills-check` fails when a vendored skill differs from upstream and
  passes when they match, in both directions and with no sibling clone present.
- Upstream CI (`scripts/validate.py`) fails on a plugin Claude Code would
  silently ignore: a skill whose frontmatter `name` does not match its
  directory, a marketplace entry with no manifest, a version the two disagree
  on, or a description too thin to route on.
- The existing guard tests keep passing: template and skill status vocabulary
  against `spec.ValidStatuses()`, skill transition arrows against
  `spec.StatusMachine`, `wf-spec-drive`'s action list against the
  `SpecTransition` switch, plan-mode lifecycle renderings against
  `spec.StatusMachine`, and track display names against `specs/README.md`
  headings.

## Outcome

Shipped 2026-08-04, directly implemented rather than dispatched. The verdict
below is the drift assessment that carries the spec through the `testing` gate;
drift is minimal.

### What shipped

`github.com/latere-ai/claude-plugins` is public and carries one plugin, `spec`,
with fourteen skills. `/plugin marketplace add latere-ai/claude-plugins` then
`/plugin install spec@latere-ai` installs it. Skills are file-first: `dispatch`,
`drive`, and `drift` describe the transition API as an accelerator under a
"when a task board is present" heading rather than assuming it, and the
remaining wallfacer paths became described concepts.

Wallfacer mirrors all fourteen into `.claude/skills/`.
`scripts/skills.sh` moves skills in either
direction, `make skills-check` gates CI, and upstream CI rejects a plugin Claude
Code would silently ignore.

### Design evolution

- **The plugin is `spec`, not `spec-workflow`.** Skills are namespaced by their
  plugin, so `spec-workflow:create` said the same word twice.
- **Two skills were renamed on the way out**, which the spec did not anticipate:
  `diff → drift` and `status → report`. Every description was also rewritten to
  lead with what separates a skill from its neighbours, since that is all a
  model sees when routing.
- **One-way `make skills-sync` became bidirectional `skills-pull` /
  `skills-push`.** A one-way sync answers "upstream changed" but not "I edited
  the copy in front of me", which is the case that actually comes up. `pull`
  also prunes vendored skills upstream no longer carries — without it the
  rename left two orphans behind.
- **Project-scoped plugin installation was tested and does not work.** A
  `.claude/settings.json` carrying `extraKnownMarketplaces` + `enabledPlugins`
  did not cause `claude -p` to fetch the marketplace, and the plugin's skills
  were absent from the session. That result is what makes the vendored mirror
  necessary rather than merely convenient.
- **The `status` collision was resolved from the plugin side.** Renaming the
  skill to `report` left the product's `/status` alone, so no user-visible
  wallfacer command moved.
- **Grouping and numbering turned out to be orthogonal.** The skills had
  encoded track directories and `NNN-` prefixes as two mutually exclusive repo
  layouts, with `validate` picking one by majority vote. They compose:
  `specs/local/003-live-serve.md` is both, and a repo may number one track and
  not another. Numbering is now judged per directory, each with its own number
  space, and `housekeeping` takes a directory as its scope rather than refusing
  when it sees track directories — which had made it unreachable in any
  grouping repo, including this one.

### Unspecified work

The guard tests that pin all three copies to `internal/spec` landed alongside
this (`8e365ac8`, `291298ef`, `9771fd51`, `03479c0e`) and are what make the
mirror maintainable rather than merely checked.
