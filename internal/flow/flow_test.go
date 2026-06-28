package flow

import (
	"testing"

	"latere.ai/x/wallfacer/internal/agents"
	"latere.ai/x/wallfacer/internal/store"
)

// TestBuiltinRegistry_HasExpectedFlows locks in the v1 catalog: the
// only built-in is "implement" after the brainstorm and test-only
// flows were retired (see specs/local/remove-idea-agent-subsystem.md).
// Keeping this strict so a typo in flows adding or removing an entry
// surfaces loudly.
func TestBuiltinRegistry_HasExpectedFlows(t *testing.T) {
	reg := NewBuiltinRegistry()
	want := map[string]bool{
		"implement": true,
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

// registryWithUserFlow returns a registry containing the "implement"
// built-in plus one user-authored flow, mirroring the merged catalog
// the dispatcher resolves against in production. Used by resolution
// tests that need a second, distinct, registered flow.
func registryWithUserFlow(t *testing.T) (*Registry, string) {
	t.Helper()
	impl, ok := NewBuiltinRegistry().Get("implement")
	if !ok {
		t.Fatal("built-in registry missing implement")
	}
	user := Flow{Slug: "custom-flow", Name: "Custom", Steps: []Step{{AgentSlug: "impl"}}}
	return NewRegistry(impl, user), user.Slug
}

// TestResolveForTask_ExplicitFlowIDWins confirms a task's explicit,
// registered FlowID resolves to itself (a user flow is not rewritten to
// the default).
func TestResolveForTask_ExplicitFlowIDWins(t *testing.T) {
	reg, userFlow := registryWithUserFlow(t)
	got := reg.ResolveForTask(&store.Task{FlowID: userFlow})
	if got != userFlow {
		t.Errorf("ResolveForTask = %q, want %q", got, userFlow)
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

// TestResolveForTask_RemovedSlugFallsBackToImplement is the regression
// guard for the fallback safety: a task pinned to a since-removed flow
// slug (the retired "brainstorm") must keep dispatching against the
// default flow rather than resolving to a slug that no longer exists.
func TestResolveForTask_RemovedSlugFallsBackToImplement(t *testing.T) {
	reg := NewBuiltinRegistry()
	if got := reg.ResolveForTask(&store.Task{FlowID: "brainstorm"}); got != "implement" {
		t.Errorf("ResolveForTask(FlowID=brainstorm) = %q, want implement", got)
	}
	if got := reg.ResolveForTask(&store.Task{FlowID: "test-only"}); got != "implement" {
		t.Errorf("ResolveForTask(FlowID=test-only) = %q, want implement", got)
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
// explicit, registered RoutineSpawnFlow resolves to that flow.
func TestResolveRoutineFlow_PrefersSpawnFlow(t *testing.T) {
	reg, userFlow := registryWithUserFlow(t)
	got := reg.ResolveRoutineFlow(&store.Task{RoutineSpawnFlow: userFlow})
	if got != userFlow {
		t.Errorf("ResolveRoutineFlow = %q, want %q", got, userFlow)
	}
}

// TestResolveRoutineFlow_RemovedSlugFallsBackToImplement is the routine
// half of the fallback-safety guard: a routine pinned to the retired
// "brainstorm" slug must keep firing against the default flow.
func TestResolveRoutineFlow_RemovedSlugFallsBackToImplement(t *testing.T) {
	reg := NewBuiltinRegistry()
	if got := reg.ResolveRoutineFlow(&store.Task{RoutineSpawnFlow: "brainstorm"}); got != "implement" {
		t.Errorf("ResolveRoutineFlow(RoutineSpawnFlow=brainstorm) = %q, want implement", got)
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
