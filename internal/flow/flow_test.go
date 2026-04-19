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

// TestResolveForTask_ExplicitFlowIDWins confirms the task's explicit
// FlowID takes precedence over its legacy Kind.
func TestResolveForTask_ExplicitFlowIDWins(t *testing.T) {
	reg := NewBuiltinRegistry()
	got := reg.ResolveForTask(&store.Task{
		Kind:   store.TaskKindIdeaAgent, // would legacy-resolve to brainstorm
		FlowID: "test-only",
	})
	if got != "test-only" {
		t.Errorf("ResolveForTask = %q, want test-only", got)
	}
}

// TestResolveForTask_EmptyFallsBackToImplement covers pre-migration
// records with no FlowID and no special Kind.
func TestResolveForTask_EmptyFallsBackToImplement(t *testing.T) {
	reg := NewBuiltinRegistry()
	got := reg.ResolveForTask(&store.Task{})
	if got != "implement" {
		t.Errorf("ResolveForTask = %q, want implement", got)
	}
}

// TestResolveForTask_IdeaAgentKindResolvesToBrainstorm covers pre-
// migration idea-agent records.
func TestResolveForTask_IdeaAgentKindResolvesToBrainstorm(t *testing.T) {
	reg := NewBuiltinRegistry()
	got := reg.ResolveForTask(&store.Task{Kind: store.TaskKindIdeaAgent})
	if got != "brainstorm" {
		t.Errorf("ResolveForTask = %q, want brainstorm", got)
	}
}

// TestResolveForTask_NilTaskReturnsDefault covers the defensive nil
// check so callers can pass task pointers without guarding.
func TestResolveForTask_NilTaskReturnsDefault(t *testing.T) {
	reg := NewBuiltinRegistry()
	if got := reg.ResolveForTask(nil); got != "implement" {
		t.Errorf("ResolveForTask(nil) = %q, want implement", got)
	}
}

// TestResolveRoutineFlow_PrefersSpawnFlow confirms a routine with an
// explicit RoutineSpawnFlow overrides any legacy RoutineSpawnKind.
func TestResolveRoutineFlow_PrefersSpawnFlow(t *testing.T) {
	reg := NewBuiltinRegistry()
	got := reg.ResolveRoutineFlow(&store.Task{
		RoutineSpawnKind: store.TaskKindIdeaAgent, // would legacy-resolve to brainstorm
		RoutineSpawnFlow: "refine-only",
	})
	if got != "refine-only" {
		t.Errorf("ResolveRoutineFlow = %q, want refine-only", got)
	}
}

// TestResolveRoutineFlow_LegacyIdeaAgentMapsToBrainstorm covers
// pre-migration routine records whose SpawnKind was idea-agent.
func TestResolveRoutineFlow_LegacyIdeaAgentMapsToBrainstorm(t *testing.T) {
	reg := NewBuiltinRegistry()
	got := reg.ResolveRoutineFlow(&store.Task{RoutineSpawnKind: store.TaskKindIdeaAgent})
	if got != "brainstorm" {
		t.Errorf("ResolveRoutineFlow = %q, want brainstorm", got)
	}
}

// TestResolveRoutineFlow_EmptyDefaultsToImplement covers routines
// with neither field set (regular implementation spawns).
func TestResolveRoutineFlow_EmptyDefaultsToImplement(t *testing.T) {
	reg := NewBuiltinRegistry()
	if got := reg.ResolveRoutineFlow(&store.Task{}); got != "implement" {
		t.Errorf("ResolveRoutineFlow = %q, want implement", got)
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
