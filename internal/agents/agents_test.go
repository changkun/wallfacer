package agents

import "testing"

func TestBuiltinAgents_AllHaveRequiredDescriptorFields(t *testing.T) {
	for _, role := range BuiltinAgents {
		if role.Slug == "" {
			t.Errorf("builtin agent missing Slug: %+v", role)
		}
		if role.Title == "" {
			t.Errorf("%s: Title is required", role.Slug)
		}
	}
}

func TestBuiltinAgents_SlugsAreUnique(t *testing.T) {
	seen := make(map[string]bool, len(BuiltinAgents))
	for _, role := range BuiltinAgents {
		if seen[role.Slug] {
			t.Errorf("duplicate builtin agent slug: %q", role.Slug)
		}
		seen[role.Slug] = true
	}
}

func TestNewBuiltinRegistry_LookupAndList(t *testing.T) {
	reg := NewBuiltinRegistry()
	for _, want := range BuiltinAgents {
		got, ok := reg.Get(want.Slug)
		if !ok {
			t.Errorf("Get(%q) missing from registry", want.Slug)
			continue
		}
		if got.Title != want.Title {
			t.Errorf("Get(%q).Title = %q, want %q", want.Slug, got.Title, want.Title)
		}
	}
	listed := reg.List()
	if len(listed) != len(BuiltinAgents) {
		t.Fatalf("List returned %d roles, want %d", len(listed), len(BuiltinAgents))
	}
}

func TestHeavyweightAgentsDeclareWriteCapability(t *testing.T) {
	for _, r := range []Role{Implementation, Testing} {
		found := false
		for _, c := range r.Capabilities {
			if c == CapWorkspaceWrite {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: expected %q in capabilities, got %v", r.Slug, CapWorkspaceWrite, r.Capabilities)
		}
	}
}
