package trajectory

import (
	"encoding/json"
	"errors"
)

// Provider identifies which CLI produced a trajectory. Kept as a string
// type so new providers can be added without touching an enum.
type Provider string

// Provider values for the adapters in this package.
const (
	ProviderClaudeCode Provider = "claude_code"
	ProviderCodex      Provider = "codex"
)

// StreamEvent is one line of an agent's NDJSON output — common shape
// across providers. Type is the primary discriminator for every
// provider; Subtype is populated for providers that use a secondary
// discriminator (Claude Code on system and result messages) and
// empty otherwise. Raw preserves the full JSON payload so callers can
// decode into a provider-specific typed variant without re-scanning.
type StreamEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`

	// Raw is the full JSON line as received. Not populated by json
	// decoding — adapters set it from the source bytes.
	Raw json.RawMessage `json:"-"`
}

// Decode unmarshals e.Raw into v. Returns ErrNoRawPayload if the event
// was hand-constructed without going through an adapter.
func (e StreamEvent) Decode(v any) error {
	if len(e.Raw) == 0 {
		return ErrNoRawPayload
	}
	return json.Unmarshal(e.Raw, v)
}

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
	// should populate it whenever the stream carries one.
	ProviderVersion string

	// Events is the ordered list of stream events. Preserves raw
	// JSON for every line so downstream code can decode into typed
	// variants or pass unknowns through unchanged.
	Events []StreamEvent
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

// ErrNoRawPayload is returned by Decode helpers when the event was
// constructed in memory without going through an adapter, so no raw
// bytes are available to unmarshal from.
var ErrNoRawPayload = errors.New("trajectory: event has no raw payload")
