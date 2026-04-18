package agents

import "testing"

// TestBuiltinAgents_AllHaveRequiredFields guards the descriptor contract:
// every built-in role must declare Activity, Name, MountMode, and a
// ParseResult. Missing any of these trips runAgent's required-field
// checks at call time; catching it at package-init time via this test
// gives us a clearer failure.
func TestBuiltinAgents_AllHaveRequiredFields(t *testing.T) {
	for _, role := range BuiltinAgents {
		if role.Name == "" {
			t.Errorf("builtin agent has empty Name: %+v", role)
		}
		if role.Activity == "" {
			t.Errorf("%s: Activity is required", role.Name)
		}
		if role.ParseResult == nil {
			t.Errorf("%s: ParseResult is required", role.Name)
		}
	}
}

// TestBuiltinAgents_SlugsAreUnique catches accidental duplicates across
// tier files. A duplicate slug would silently overwrite an earlier
// Registry entry and break lookup.
func TestBuiltinAgents_SlugsAreUnique(t *testing.T) {
	seen := make(map[string]bool, len(BuiltinAgents))
	for _, role := range BuiltinAgents {
		if seen[role.Name] {
			t.Errorf("duplicate builtin agent slug: %q", role.Name)
		}
		seen[role.Name] = true
	}
}

// TestNewBuiltinRegistry_LookupAndList exercises the registry surface:
// every built-in is reachable via Get, and List returns them in
// registration order.
func TestNewBuiltinRegistry_LookupAndList(t *testing.T) {
	reg := NewBuiltinRegistry()
	for _, want := range BuiltinAgents {
		got, ok := reg.Get(want.Name)
		if !ok {
			t.Errorf("Get(%q) missing from registry", want.Name)
			continue
		}
		if got.Activity != want.Activity {
			t.Errorf("Get(%q).Activity = %q, want %q", want.Name, got.Activity, want.Activity)
		}
	}

	listed := reg.List()
	if len(listed) != len(BuiltinAgents) {
		t.Fatalf("List returned %d roles, want %d", len(listed), len(BuiltinAgents))
	}
	for i, r := range listed {
		if r.Name != BuiltinAgents[i].Name {
			t.Errorf("List[%d] = %q, want %q", i, r.Name, BuiltinAgents[i].Name)
		}
	}
}
