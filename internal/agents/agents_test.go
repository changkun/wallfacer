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

// TestBuiltinAgents_NoIdeateAgent guards against reintroduction of the
// retired brainstorm/ideate built-in agent (see
// specs/local/remove-idea-agent-subsystem.md). The ideate role was
// removed with the idea-agent subsystem.
func TestBuiltinAgents_NoIdeateAgent(t *testing.T) {
	for _, role := range BuiltinAgents {
		if role.Slug == "ideate" {
			t.Errorf("retired %q agent must not be a built-in", role.Slug)
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

// TestBuiltinAgents_AreTheSixRoles pins the table to the six roles the
// built-in pipeline runs, and checks none carries a harness pin: a pin is set
// per call on a copy, never on the shared descriptor.
func TestBuiltinAgents_AreTheSixRoles(t *testing.T) {
	want := []string{"title", "oversight", "commit-msg", "impl", "test", "review"}
	if len(BuiltinAgents) != len(want) {
		t.Fatalf("BuiltinAgents has %d roles, want %d", len(BuiltinAgents), len(want))
	}
	for i, slug := range want {
		if BuiltinAgents[i].Slug != slug {
			t.Errorf("BuiltinAgents[%d].Slug = %q, want %q", i, BuiltinAgents[i].Slug, slug)
		}
		if BuiltinAgents[i].Harness != "" {
			t.Errorf("%s: Harness = %q, want empty on the built-in descriptor", slug, BuiltinAgents[i].Harness)
		}
	}
}
