package flow

import (
	"testing"

	"changkun.de/x/wallfacer/internal/agents"
	"changkun.de/x/wallfacer/internal/store"
)

// TestBuiltinRegistry_HasExpectedFlows locks in the v1 catalog: every
// built-in must be present and no extras. Keeping this strict so a
// typo in flows adding or removing an entry surfaces loudly.
func TestBuiltinRegistry_HasExpectedFlows(t *testing.T) {
	reg := NewBuiltinRegistry()
	want := map[string]bool{
		"implement":   true,
		"brainstorm":  true,
		"refine-only": true,
		"test-only":   true,
	}
	got := reg.List()
	if len(got) != len(want) {
		t.Fatalf("List length = %d, want %d: %+v", len(got), len(want), got)
	}
	seen := make(map[string]bool)
	for _, f := range got {
		if !want[f.Slug] {
			t.Errorf("unexpected slug %q in built-ins", f.Slug)
		}
		if seen[f.Slug] {
			t.Errorf("duplicate slug %q", f.Slug)
		}
		seen[f.Slug] = true
		if !f.Builtin {
			t.Errorf("%s: Builtin = false, want true", f.Slug)
		}
	}
}

// TestBuiltinRegistry_ImplementReferencesRealAgents is the cross-
// package sanity check: every AgentSlug on every built-in step must
// resolve in the agents registry. Catches renames on either side
// before they reach runtime.
func TestBuiltinRegistry_ImplementReferencesRealAgents(t *testing.T) {
	agentReg := agents.NewBuiltinRegistry()
	for _, f := range NewBuiltinRegistry().List() {
		for i, s := range f.Steps {
			if _, ok := agentReg.Get(s.AgentSlug); !ok {
				t.Errorf("flow %s step %d references unknown agent %q",
					f.Slug, i, s.AgentSlug)
			}
		}
	}
}

// TestRegistry_ResolveLegacyKind_MapsEmptyToImplement covers the
// default legacy task-kind.
func TestRegistry_ResolveLegacyKind_MapsEmptyToImplement(t *testing.T) {
	reg := NewBuiltinRegistry()
	f, ok := reg.ResolveLegacyKind("")
	if !ok {
		t.Fatal("ResolveLegacyKind(\"\") returned ok=false")
	}
	if f.Slug != "implement" {
		t.Errorf("slug = %q, want implement", f.Slug)
	}
}

// TestRegistry_ResolveLegacyKind_MapsIdeaAgentToBrainstorm covers the
// idea-agent legacy task-kind.
func TestRegistry_ResolveLegacyKind_MapsIdeaAgentToBrainstorm(t *testing.T) {
	reg := NewBuiltinRegistry()
	f, ok := reg.ResolveLegacyKind(store.TaskKindIdeaAgent)
	if !ok {
		t.Fatal("ResolveLegacyKind(idea-agent) returned ok=false")
	}
	if f.Slug != "brainstorm" {
		t.Errorf("slug = %q, want brainstorm", f.Slug)
	}
}

// TestRegistry_ResolveLegacyKind_UnknownReturnsFalse guards the
// deliberate falseness for kinds that don't yet have a flow mapping
// (planning, routine). If this test starts failing, the caller needs
// to explicitly opt the new kind into the resolver.
func TestRegistry_ResolveLegacyKind_UnknownReturnsFalse(t *testing.T) {
	reg := NewBuiltinRegistry()
	if _, ok := reg.ResolveLegacyKind(store.TaskKindPlanning); ok {
		t.Error("ResolveLegacyKind(planning) returned ok=true; want false until migrated")
	}
	if _, ok := reg.ResolveLegacyKind(store.TaskKindRoutine); ok {
		t.Error("ResolveLegacyKind(routine) returned ok=true; want false until migrated")
	}
}

// TestRegistry_ListReturnsDeepCopy catches accidental sharing of the
// Steps slice between List output and registry state — mutating a
// returned Flow must not leak back.
func TestRegistry_ListReturnsDeepCopy(t *testing.T) {
	reg := NewBuiltinRegistry()

	list1 := reg.List()
	// Mutate the first list: blank out Steps, drop a RunInParallelWith.
	for i := range list1 {
		list1[i].Slug = "mutated"
		list1[i].Steps = nil
	}

	list2 := reg.List()
	for _, f := range list2 {
		if f.Slug == "mutated" {
			t.Errorf("registry state leaked: got slug %q", f.Slug)
		}
		if f.Slug == "implement" && len(f.Steps) == 0 {
			t.Errorf("registry state leaked: implement's Steps were cleared")
		}
	}

	// Also confirm parallel-with slices aren't aliased: mutating one
	// entry's RunInParallelWith must not affect subsequent calls.
	f1, _ := reg.Get("implement")
	for i := range f1.Steps {
		if len(f1.Steps[i].RunInParallelWith) > 0 {
			f1.Steps[i].RunInParallelWith[0] = "tainted"
		}
	}
	f2, _ := reg.Get("implement")
	for i, s := range f2.Steps {
		for _, p := range s.RunInParallelWith {
			if p == "tainted" {
				t.Errorf("implement Steps[%d].RunInParallelWith aliased: %v", i, s.RunInParallelWith)
			}
		}
	}
}
