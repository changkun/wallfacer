package prompts

import "testing"

// ---------------------------------------------------------------------------
// InstructionsKey
// ---------------------------------------------------------------------------

// TestInstructionsKeyStable verifies that the same workspace list always
// produces the same key.
func TestInstructionsKeyStable(t *testing.T) {
	ws := []string{"/home/user/projectA", "/home/user/projectB"}
	k1 := InstructionsKey(ws)
	k2 := InstructionsKey(ws)
	if k1 != k2 {
		t.Fatalf("key should be stable: got %q then %q", k1, k2)
	}
}

// TestInstructionsKeyOrderIndependent verifies that workspace order does not
// affect the key, so wallfacer run ~/a ~/b and wallfacer run ~/b ~/a share
// the same key.
func TestInstructionsKeyOrderIndependent(t *testing.T) {
	ws1 := []string{"/home/user/alpha", "/home/user/beta"}
	ws2 := []string{"/home/user/beta", "/home/user/alpha"}
	if InstructionsKey(ws1) != InstructionsKey(ws2) {
		t.Fatalf("key must be order-independent: %q != %q", InstructionsKey(ws1), InstructionsKey(ws2))
	}
}

// TestInstructionsKeyDifferentWorkspaces verifies that distinct workspace sets
// produce distinct keys.
func TestInstructionsKeyDifferentWorkspaces(t *testing.T) {
	k1 := InstructionsKey([]string{"/home/user/foo"})
	k2 := InstructionsKey([]string{"/home/user/bar"})
	if k1 == k2 {
		t.Fatalf("different workspaces should produce different keys, both got %q", k1)
	}
}

// TestInstructionsKeyLength verifies the key is exactly 16 hex characters.
func TestInstructionsKeyLength(t *testing.T) {
	k := InstructionsKey([]string{"/some/path"})
	if len(k) != 16 {
		t.Fatalf("expected 16-char key, got %d chars: %q", len(k), k)
	}
}
