package coordinator

import (
	"encoding/json"
	"testing"
)

func TestDecodeEnvelope(t *testing.T) {
	t.Run("known type retains raw", func(t *testing.T) {
		b := []byte(`{"type":"manifest","instance_id":"inst_1"}`)
		e, err := DecodeEnvelope(b)
		if err != nil {
			t.Fatalf("DecodeEnvelope: %v", err)
		}
		if e.Type != FrameManifest {
			t.Fatalf("type = %q, want %q", e.Type, FrameManifest)
		}
		var m Manifest
		if err := json.Unmarshal(e.Raw, &m); err != nil {
			t.Fatalf("second decode: %v", err)
		}
		if m.InstanceID != "inst_1" {
			t.Fatalf("instance_id = %q, want inst_1", m.InstanceID)
		}
	})

	t.Run("unknown type is not an error", func(t *testing.T) {
		// A newer peer's frame must decode to its type, not fail, so the
		// dispatcher can ignore it without dropping the connection.
		e, err := DecodeEnvelope([]byte(`{"type":"future-capability"}`))
		if err != nil {
			t.Fatalf("unknown type should decode, got error: %v", err)
		}
		if e.Type != "future-capability" {
			t.Fatalf("type = %q", e.Type)
		}
	})

	t.Run("empty type is an error", func(t *testing.T) {
		if _, err := DecodeEnvelope([]byte(`{"instance_id":"x"}`)); err == nil {
			t.Fatal("expected error for missing type")
		}
	})

	t.Run("malformed json is an error", func(t *testing.T) {
		if _, err := DecodeEnvelope([]byte(`{not json`)); err == nil {
			t.Fatal("expected error for malformed json")
		}
	})
}

func TestManifestRoundTrip(t *testing.T) {
	m := NewManifest("inst_1", "changkun-mbp", "wallfacer/1.2.3",
		[]WorkspaceRef{{Remote: "github.com/latere-ai/wallfacer", LocalKey: "k1"}},
		[]string{"presence", "comments"})

	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// No principal/org on the wire: the coordinator derives them from the JWT.
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, forbidden := range []string{"principal", "org", "sub", "org_id"} {
		if _, ok := raw[forbidden]; ok {
			t.Errorf("manifest must not carry %q on the wire", forbidden)
		}
	}

	var got Manifest
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Type != FrameManifest {
		t.Errorf("type = %q, want %q", got.Type, FrameManifest)
	}
	if got.InstanceID != "inst_1" || got.HostLabel != "changkun-mbp" {
		t.Errorf("fields lost: %+v", got)
	}
}

func TestManifestHelpers(t *testing.T) {
	m := NewManifest("i", "h", "v",
		[]WorkspaceRef{
			{Remote: "github.com/a/b", LocalKey: "k1"},
			{Remote: "", LocalKey: "k2"}, // remote-less workspace, excluded
			{Remote: "github.com/c/d", LocalKey: "k3"},
		},
		[]string{"presence", "projection"})

	got := m.Remotes()
	want := []string{"github.com/a/b", "github.com/c/d"}
	if len(got) != len(want) {
		t.Fatalf("Remotes() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Remotes()[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	if !m.HasCapability("presence") {
		t.Error("HasCapability(presence) = false")
	}
	if m.HasCapability("comments") {
		t.Error("HasCapability(comments) = true, want false")
	}
}
