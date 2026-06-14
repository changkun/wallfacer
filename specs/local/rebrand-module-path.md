---
title: Rebrand Module Path to latere.ai
status: complete
depends_on: []
affects:
  - go.mod
  - Makefile
  - "**/*.go"
effort: medium
created: 2026-03-28
updated: 2026-06-14
author: changkun
dispatched_task_id: null
---

# Rebrand Module Path to latere.ai

## Goal

Migrate the wallfacer project identity from `changkun.de/x/wallfacer` to `latere.ai/x/wallfacer`, aligning the product under the latere.ai brand.

The `/x/` segment mirrors the sibling latere Go modules (`latere.ai/x/agents`, `latere.ai/x/auth`, `latere.ai/x/fs`, `latere.ai/x/lux`, `latere.ai/x/pkg`, `latere.ai/x/sandbox`) and the old `changkun.de/x/wallfacer` structure.

## Scope

| Area | Current | Target |
|------|---------|--------|
| Go module path | `changkun.de/x/wallfacer` | `latere.ai/x/wallfacer` |
| Sandbox images | — | `ghcr.io/latere-ai/sandbox-agents` (done — moved to `github.com/latere-ai/images`) |
| App image (`wallfacer web`/server) | `ghcr.io/changkun/wallfacerd` | **Deferred — stays `ghcr.io/changkun/wallfacerd`** (see Open Questions; referenced in `wallfacerd.yml`, `deploy-wallfacerd.yml`, `deploy/prod/deployment.yaml`) |
| macOS bundle ID | `ai.latere.wallfacer` | Already correct (set in desktop-app task-08) |
| Import statements | ~200 files | Bulk rename |
| CI ldflags | Makefile, release-binary.yml | Update module prefix |
| Docs | CLAUDE.md, AGENTS.md, doc.go files | Update references |

## Approach

1. `go mod edit -module latere.ai/x/wallfacer`
2. Bulk find-replace `changkun.de/x/wallfacer` → `latere.ai/x/wallfacer` across all `.go` files
3. Update Makefile ldflags, CI workflows, documentation
4. ~~Update container image base path~~ — deferred; `wallfacerd` image stays at `ghcr.io/changkun` (see Open Questions)
5. Run `go build ./...` and `go test ./...` to verify
6. Consider adding a `go.mod` retract or vanity import redirect at the old path

## Open Questions

- ~~Target container registry org~~ — sandbox images already migrated (`ghcr.io/latere-ai/sandbox-agents`). The `wallfacerd` app image **stays at `ghcr.io/changkun/wallfacerd` for now**: the repo is `github.com/changkun/wallfacer` and CI pushes with the default `GITHUB_TOKEN` (`packages: write`), which can write to `ghcr.io/changkun/*` but not the `latere-ai` org. Renaming to `ghcr.io/latere-ai/wallfacerd` is blocked until the repo moves under the `latere-ai` org or an org PAT secret grants this repo GHCR package-write. Revisit then.
- ~~Whether to set up a vanity import server at `latere.ai/x/wallfacer`~~ — resolved: the `latere-ai` site already serves `go-import` meta tags for `latere.ai/x/<repo>` (`internal/handler/handler.go`). Because that handler hardcodes the `latere-ai` GitHub org but wallfacer still lives at `github.com/changkun/wallfacer`, a `vanityOwners["wallfacer"] = "changkun"` override was added there so `go get latere.ai/x/wallfacer` and `git clone https://latere.ai/x/wallfacer` resolve correctly. Remove the override once wallfacer moves into the `latere-ai` org (same precondition as the `wallfacerd` image rename above).
- Timing relative to other work (standalone migration or bundled with a release)

## Outcome

Shipped (2026-06-14):

- **Module path migrated** to `latere.ai/x/wallfacer` (chose the `/x/` form to match sibling latere modules). Rewrote go.mod, all 267 importing `.go` files, Makefile ldflags, `release-binary.yml`/`release-desktop.yml`, dev docs, and stale spec references. Verified with `go build`, `go vet`, `make lint-go` (0 issues), and `go test ./...`. (`ffce4807`)
- **Vanity import works** for `go get latere.ai/x/wallfacer`: added a `vanityOwners` override in the `latere-ai` site's `go-import` handler mapping wallfacer to the `changkun` GitHub owner, with tests for the meta tag and the `git clone` `info/refs` redirect. Released as `latere-ai` `v0.2.78`. (`latere-ai@7e21440`)

Deferred (both blocked on the same precondition — wallfacer moving into the `latere-ai` GitHub org, or an org PAT granting this repo access):

- `wallfacerd` app image stays at `ghcr.io/changkun/wallfacerd`; `deployment.yaml` and the wallfacerd workflows are unchanged.
- The `vanityOwners["wallfacer"]` override remains until the repo migrates, after which it should be removed.

Marked `complete` (2026-06-14): the primary deliverable (module path
migration) shipped and is verified. The two follow-ups above are external,
gated on the `wallfacer` repo moving into the `latere-ai` GitHub org, and
are tracked here as Deferred rather than blocking completion.
