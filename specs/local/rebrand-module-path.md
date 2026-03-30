---
title: Rebrand Module Path to latere.ai
status: drafted
depends_on: []
affects:
  - go.mod
  - Makefile
  - "**/*.go"
effort: medium
created: 2026-03-28
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Rebrand Module Path to latere.ai

## Goal

Migrate the wallfacer project identity from `changkun.de/x/wallfacer` to `latere.ai/wallfacer`, aligning the product under the latere.ai brand.

## Scope

| Area | Current | Target |
|------|---------|--------|
| Go module path | `changkun.de/x/wallfacer` | `latere.ai/wallfacer` |
| Container images | `ghcr.io/changkun/wallfacer` | TBD (`ghcr.io/latere-ai/wallfacer`?) |
| macOS bundle ID | `ai.latere.wallfacer` | Already correct (set in desktop-app task-08) |
| Import statements | ~200 files | Bulk rename |
| CI ldflags | Makefile, release-binary.yml | Update module prefix |
| Docs | CLAUDE.md, AGENTS.md, doc.go files | Update references |

## Approach

1. `go mod edit -module latere.ai/wallfacer`
2. Bulk find-replace `changkun.de/x/wallfacer` → `latere.ai/wallfacer` across all `.go` files
3. Update Makefile ldflags, CI workflows, documentation
4. Update container image base path if registry org changes
5. Run `go build ./...` and `go test ./...` to verify
6. Consider adding a `go.mod` retract or vanity import redirect at the old path

## Open Questions

- Target container registry org (keep `ghcr.io/changkun` or move to `ghcr.io/latere-ai`?)
- Whether to set up a vanity import server at `latere.ai/wallfacer` (like `golang.org/x/` style)
- Timing relative to other work (standalone migration or bundled with a release)
