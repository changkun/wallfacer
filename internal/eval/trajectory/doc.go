// Package trajectory converts raw agent stream output (Claude Code's
// --output-format stream-json, Codex's codex exec --json) into a stable
// internal representation suitable for offline evaluation.
//
// The public contract of an agent stream is vendor-owned and subject to
// change across CLI releases. The adapters in this package isolate that
// churn: each adapter owns the quirks of one provider, decodes a line
// into a typed variant when possible, and preserves the raw JSON for
// variants not yet modeled so downstream code never blocks on an
// unrecognized event type.
//
// Claude Code message schemas in this package mirror the Zod definitions
// in the claude-code source under src/entrypoints/sdk/coreSchemas.ts.
// When the upstream schema changes, add new typed variants here, do not
// silently patch the existing ones — the point is to notice drift.
package trajectory
