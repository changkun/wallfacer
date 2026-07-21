// Package harness defines the abstraction over coding-agent CLIs
// (Claude Code, Codex, Cursor, OpenCode, Pi, …) that wallfacer drives.
//
// A [Harness] implementation owns three things: how to build the CLI's
// argv from a canonical [Request], how to parse one line of its
// NDJSON event stream into a canonical [Event], and how to project
// stored credentials into the env vars the CLI expects.
//
// The package also exposes a [Registry] for ID-keyed lookup and a
// [FakeHarness] for tests in dependent packages.
//
// This package is the canonical agent-CLI abstraction wired throughout
// production: the runner resolves a [Harness] from the [Registry] to build
// argv, parse the NDJSON event stream, and project credentials for every
// sub-agent launch.
package harness
