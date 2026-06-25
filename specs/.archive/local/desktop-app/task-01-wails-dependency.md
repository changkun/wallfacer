---
title: Add Wails v2 Dependency and Project Scaffold
status: archived
depends_on: []
affects: []
effort: small
created: 2026-03-28
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---


# Task 1: Add Wails v2 Dependency and Project Scaffold

## Goal

Add the Wails v2 module dependency and create the minimal project structure needed for the desktop app, without changing any existing behavior.

## What to do

1. Run `go get github.com/wailsapp/wails/v2` to add the dependency to `go.mod`
2. Create `internal/cli/desktop.go` with a placeholder `RunDesktop()` function that:
   - Accepts the same `uiFiles` and `docsFiles` `embed.FS` parameters as `RunServer`
   - Logs "desktop mode not yet implemented" and returns an error
3. Create `internal/cli/desktop_stub.go` with build tag `//go:build !desktop` that provides a stub `RunDesktop()` returning an "unsupported" error — this keeps the default `go build` clean of CGo/Wails dependencies
4. Add the `desktop` case to the subcommand switch in `main.go`, calling `cli.RunDesktop(uiFiles, docsFiles)`
5. Verify `go build ./...` still works without the `desktop` build tag
6. Verify `go build -tags desktop ./...` compiles with the Wails dependency

## Tests

- `TestRunDesktopStub`: Call `RunDesktop()` without the desktop build tag, assert it returns an error containing "unsupported" or "not yet implemented"
- `go vet ./...` passes with and without the `desktop` build tag

## Boundaries

- Do NOT modify `RunServer()` or any existing HTTP handler
- Do NOT add system tray, window management, or any Wails runtime code yet
- Do NOT add icons, packaging, or build targets
