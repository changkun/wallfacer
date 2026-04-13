package trajectory

import "errors"

// Provider identifies which CLI produced a trajectory. Kept as a string
// type so new providers can be added without touching an enum.
type Provider string

// Provider values for the adapters in this package.
const (
	ProviderClaudeCode Provider = "claude_code"
	ProviderCodex      Provider = "codex"
)

// Trajectory is the internal, vendor-agnostic representation of a
// single agent run. Adapters decode raw provider streams into this
// shape; metric and judge pipelines consume it.
//
// The schema is intentionally minimal for now — enough to hang rule-
// based metrics off. Richer structure (per-turn rollups, tool-call
// graph, reward attribution) grows as the eval pipeline evolves.
type Trajectory struct {
	// Provider identifies which adapter produced this trajectory.
	Provider Provider

	// ProviderVersion is the CLI version string the trajectory came
	// from (e.g. "claude-code/1.2.3"). Empty when unknown; adapters
	// should populate it whenever a system-init message carries one.
	ProviderVersion string

	// Messages is the ordered list of SDK messages from the stream.
	// Preserves raw JSON for every line so downstream code can decode
	// into typed variants or pass through unknowns.
	Messages []SDKMessage
}

// Adapter converts a raw NDJSON byte stream into a Trajectory.
// Implementations must be pure (no I/O, no globals) so they can be
// tested offline against fixture files.
type Adapter interface {
	// Provider reports which provider the adapter handles.
	Provider() Provider

	// Parse decodes rawNDJSON into a Trajectory. Lines that fail to
	// decode return an error identifying the line number; the caller
	// decides whether to continue past partial data.
	Parse(rawNDJSON []byte) (Trajectory, error)
}

// ErrNoRawPayload is returned by typed Decode helpers when the SDK
// message was constructed in memory without going through an adapter,
// so no raw bytes are available to unmarshal from.
var ErrNoRawPayload = errors.New("trajectory: message has no raw payload")
