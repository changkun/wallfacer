// Package coordinator implements the cloud coordination plane: the wallfacerd
// role that signed-in local wallfacer instances connect to over a single
// outbound WebSocket for presence, remote control, metadata projection, and
// spec-comment collaboration.
//
// This file defines the wire contract shared by the accept side (the
// coordinator) and the dial side (the local instance client). See
// specs/cloud/latere-integration/coordination-plane/connection-and-presence/connection.md
// for the design.
package coordinator

import (
	"encoding/json"
	"fmt"
	"slices"
)

// Frame type discriminators. Every frame on the connection is a JSON object
// whose "type" selects the payload. The manifest is defined here; capability
// frame types (presence, projection, command, spec-comment) are reserved so a
// newer peer does not break an older one when it sees an unknown type.
const (
	FrameManifest         = "manifest"
	FramePresence         = "presence"          // instance -> coordinator focus/typing delta
	FramePresenceSnapshot = "presence-snapshot" // coordinator -> instance org aggregate
	FrameProjection       = "projection"        // instance -> coordinator metadata push
	FrameCommand          = "command"           // coordinator -> instance remote-control action
	FrameSpecComment      = "spec-comment"      // bidirectional comment event
)

// Envelope is the minimal shape decoded first to dispatch on Type. The
// remaining bytes are decoded into the concrete payload by the capability that
// owns the frame type.
type Envelope struct {
	Type string          `json:"type"`
	Raw  json.RawMessage `json:"-"`
}

// DecodeEnvelope reads the type discriminator and retains the raw bytes for a
// second, payload-specific decode. An empty type is an error; an unknown but
// non-empty type is the caller's concern (the dispatcher ignores it with a
// warning rather than dropping the connection).
func DecodeEnvelope(b []byte) (Envelope, error) {
	var e struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(b, &e); err != nil {
		return Envelope{}, fmt.Errorf("decode envelope: %w", err)
	}
	if e.Type == "" {
		return Envelope{}, fmt.Errorf("frame has no type")
	}
	return Envelope{Type: e.Type, Raw: append(json.RawMessage(nil), b...)}, nil
}

// Manifest is the first frame an instance sends after the handshake. It carries
// only registration metadata. Crucially, it does NOT carry the principal or
// org: the coordinator derives those from the validated JWT so a client cannot
// claim another principal.
type Manifest struct {
	Type         string         `json:"type"` // always FrameManifest
	InstanceID   string         `json:"instance_id"`
	HostLabel    string         `json:"host_label"`
	Version      string         `json:"version"`
	Workspaces   []WorkspaceRef `json:"workspaces"`
	Capabilities []string       `json:"capabilities"`
}

// WorkspaceRef identifies a workspace an instance currently serves. Remote is
// the cross-machine join key (canonical git remote URL, see NormalizeRemoteURL);
// LocalKey is the per-machine workspace.GroupKey, opaque to the coordinator and
// used only for the instance's own routing. A workspace with no git remote has
// an empty Remote and never joins org collaboration.
type WorkspaceRef struct {
	Remote   string `json:"remote"`
	LocalKey string `json:"local_key"`
}

// Principal is the validated identity of a connection, taken from the JWT, never
// from the manifest body. It is the registry key.
type Principal struct {
	Sub   string
	OrgID string
}

// NewManifest builds a manifest frame with the type tag set.
func NewManifest(instanceID, hostLabel, version string, ws []WorkspaceRef, caps []string) Manifest {
	return Manifest{
		Type:         FrameManifest,
		InstanceID:   instanceID,
		HostLabel:    hostLabel,
		Version:      version,
		Workspaces:   ws,
		Capabilities: caps,
	}
}

// Remotes returns the non-empty cross-machine workspace keys in the manifest.
func (m Manifest) Remotes() []string {
	var out []string
	for _, w := range m.Workspaces {
		if w.Remote != "" {
			out = append(out, w.Remote)
		}
	}
	return out
}

// HasCapability reports whether the instance advertised the given capability.
func (m Manifest) HasCapability(c string) bool {
	return slices.Contains(m.Capabilities, c)
}
