package prompts

import (
	"regexp"
	"testing"
)

// ---------------------------------------------------------------------------
// WorkspaceDataKey
// ---------------------------------------------------------------------------

// TestWorkspaceDataKeyStable verifies that the same workspace list always
// produces the same key.
func TestWorkspaceDataKeyStable(t *testing.T) {
	ws := []string{"/home/user/projectA", "/home/user/projectB"}
	k1 := WorkspaceDataKey(ws)
	k2 := WorkspaceDataKey(ws)
	if k1 != k2 {
		t.Fatalf("key should be stable: got %q then %q", k1, k2)
	}
}

// TestWorkspaceDataKeyOrderIndependent verifies that workspace order does not
// affect the key, so wallfacer run ~/a ~/b and wallfacer run ~/b ~/a share
// the same key.
func TestWorkspaceDataKeyOrderIndependent(t *testing.T) {
	ws1 := []string{"/home/user/alpha", "/home/user/beta"}
	ws2 := []string{"/home/user/beta", "/home/user/alpha"}
	if WorkspaceDataKey(ws1) != WorkspaceDataKey(ws2) {
		t.Fatalf("key must be order-independent: %q != %q", WorkspaceDataKey(ws1), WorkspaceDataKey(ws2))
	}
}

// TestWorkspaceDataKeyDifferentWorkspaces verifies that distinct workspace sets
// produce distinct keys.
func TestWorkspaceDataKeyDifferentWorkspaces(t *testing.T) {
	k1 := WorkspaceDataKey([]string{"/home/user/foo"})
	k2 := WorkspaceDataKey([]string{"/home/user/bar"})
	if k1 == k2 {
		t.Fatalf("different workspaces should produce different keys, both got %q", k1)
	}
}

// TestWorkspaceDataKeyLength verifies the key is exactly 16 hex characters.
func TestWorkspaceDataKeyLength(t *testing.T) {
	k := WorkspaceDataKey([]string{"/some/path"})
	if len(k) != 16 {
		t.Fatalf("expected 16-char key, got %d chars: %q", len(k), k)
	}
}

// ---------------------------------------------------------------------------
// NewDataKey
// ---------------------------------------------------------------------------

var hex16 = regexp.MustCompile(`^[0-9a-f]{16}$`)

// TestNewDataKeyShape verifies a fresh key is 16 lowercase hex chars, matching
// the width of WorkspaceDataKey so both address data/<key> uniformly.
func TestNewDataKeyShape(t *testing.T) {
	k := NewDataKey()
	if !hex16.MatchString(k) {
		t.Fatalf("expected 16-char hex key, got %q", k)
	}
}

// TestNewDataKeyUnique verifies successive calls return distinct keys: identity
// is independent of the folder set, so two workspaces never collide by storage.
func TestNewDataKeyUnique(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := range 1000 {
		k := NewDataKey()
		if seen[k] {
			t.Fatalf("NewDataKey returned a duplicate key %q after %d draws", k, i)
		}
		seen[k] = true
	}
}

// TestNewDataKeyIndependentOfPaths verifies a new key is NOT derived from any
// folder set: it must differ from WorkspaceDataKey of an arbitrary path set, so
// a new workspace sharing folders with a migrated one starts with empty history.
func TestNewDataKeyIndependentOfPaths(t *testing.T) {
	seeded := WorkspaceDataKey([]string{"/home/user/projectA"})
	for range 100 {
		if NewDataKey() == seeded {
			t.Fatal("NewDataKey collided with a path-seeded key; identity is not independent of folders")
		}
	}
}
