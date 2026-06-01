// Package sandbox defines the host process launch abstraction (Backend /
// Handle / ContainerSpec) used by the runner.
//
// The agent-type enum now lives in [harness]; the names below are
// backward-compatible aliases retained while call sites migrate to
// harness.ID directly. New code should use harness.ID / harness.Claude /
// harness.Codex.
package sandbox

import "changkun.de/x/wallfacer/internal/harness"

// Type aliases harness.ID. It is the SAME type, not a distinct one, so the
// two enums can no longer drift. Deprecated: use harness.ID.
type Type = harness.ID

// Agent-type constants, aliased to the harness package.
const (
	Claude = harness.Claude
	Codex  = harness.Codex
)

// All returns the registered harness IDs, sorted. Deprecated: use harness.All.
func All() []Type { return harness.All() }

// Parse parses value into a registered harness ID. Deprecated: use harness.ParseID.
func Parse(value string) (Type, bool) { return harness.ParseID(value) }

// Normalize returns the canonical lowercase ID. Deprecated: use harness.NormalizeID.
func Normalize(value string) Type { return harness.NormalizeID(value) }

// Default returns the parsed ID or the default harness. Deprecated: use harness.DefaultFrom.
func Default(value string) Type { return harness.DefaultFrom(value) }
