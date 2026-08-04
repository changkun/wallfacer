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

Create `github.com/latere-ai/claude-plugins` as a marketplace hosting one or
more plugins. The spec workflow ships as the first, named `spec-workflow`.

```
claude-plugins/
  .claude-plugin/marketplace.json     # name, owner, plugins[] with source paths
  plugins/spec-workflow/
    .claude-plugin/plugin.json        # name, version, description, author
    skills/<name>/SKILL.md            # one directory per skill
    README.md
    LICENSE
```

Installation is two steps:

```
/plugin marketplace add latere-ai/claude-plugins
/plugin install spec-workflow@latere-ai
```

### 2. Command naming

Plugin skills are namespaced by their plugin, so `wf-spec-create` becomes
`spec-workflow:create`. Drop the `wf-spec-` prefix from every skill directory;
the namespace carries it.

### 3. Server coupling

Ten of the thirteen skills are pure markdown, git, and frontmatter operations
and port unchanged. Three reach for wallfacer's HTTP API:

| Skill | Endpoint |
| --- | --- |
| `dispatch` | `POST /api/specs/transition`, `POST /api/tasks` |
| `drive` | `POST /api/specs/transition`, `GET /api/tasks/{id}` |
| `diff` | `GET /api/tasks/{id}/diff` |

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

Wallfacer keeps `.claude/skills/` committed: the Claude harness runs
`claude -p` in containers where no user-level plugin is installed, and dropping
the committed copy would strip dispatched tasks of the workflow. Upstream
becomes canonical and the vendored copy is a mirror.

- `make skills-sync` pulls the upstream plugin's `skills/` into
  `.claude/skills/`, rewriting names to the vendored convention.
- `make skills-check` diffs the two and exits non-zero on any difference,
  naming the drifted files. Wired into CI.

### 6. Product template convergence

The twelve `commands_templates/*.tmpl` prompts and the thirteen skills overlap
but do not match: no template for `drive`, `implement`, or `housekeeping`; no
skill for `summarize`; and `/status` means "set this spec's status" in the
product against "report across all specs" in the skill.

This spec does **not** converge them. The templates stay a deliberately thinner
surface for in-product chat, where the focused spec is already known and the
server is always reachable. The name collision on `status` is resolved by
renaming the product command to `/set-status`, leaving `status` free to mean the
same thing in both surfaces.

## Out of scope

- Publishing any other skill (`prose-style`, `deep-tech-book`, `check-docs`) as
  a plugin. The marketplace layout leaves room; adding them is separate work.
- `wf-spec-housekeeping`, which only applies to flat-numbered spec trees and is
  currently global-only. It ships with the plugin but wallfacer does not vendor
  it.
- Changing the lifecycle state machine or the transition API.

## Tests

- `make skills-check` fails when a vendored skill differs from upstream and
  passes when they match.
- A test asserts every skill directory named in the plugin manifest exists and
  carries frontmatter with `name` and `description`.
- The existing guard tests keep passing: template status vocabulary against
  `spec.ValidStatuses()`, plan-mode lifecycle renderings against
  `spec.StatusMachine`, track display names against `specs/README.md` headings.
- Renaming `/status` to `/set-status` keeps `commands_test.go`'s registry
  assertions green with the new name.

## Open questions

- Does the marketplace repository need to be public for `/plugin marketplace
  add latere-ai/claude-plugins` to resolve, or is a private repo reachable
  through the user's GitHub auth?
- Should the vendored copy live at `.claude/skills/` (auto-discovered) or be
  installed from a pinned plugin version at container build time? The former is
  simpler and is what this spec assumes.
