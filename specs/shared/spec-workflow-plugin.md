---
title: Spec Workflow as an Installable Plugin
status: drafted
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

`wf-spec-housekeeping` is deliberately not vendored: it only applies to
flat-numbered trees.

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
  a plugin. The marketplace layout leaves room; adding them is separate work.
- `wf-spec-housekeeping`, which only applies to flat-numbered spec trees. It
  ships with the plugin but wallfacer does not vendor it.
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

## Open questions

- Should the vendored copy stay a mirror at `.claude/skills/`, or be installed
  from a pinned plugin version at container build time once Claude Code can do
  that headlessly? The mirror is what shipped.
- Should the plugin carry the other repo-agnostic skills (`prose-style`,
  `deep-tech-book`, `check-docs`) as siblings under the same marketplace, or do
  those want a marketplace of their own?
