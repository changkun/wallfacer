package flow

import "latere.ai/x/wallfacer/internal/store"

// Registry is the merged catalog of built-in and (future) user-
// authored flows. Today it wraps the embedded built-ins and exposes a
// lookup + listing surface; the user-authored loader lands on the
// editable-flows task.
type Registry struct {
	order []string
	byKey map[string]Flow
}

// NewBuiltinRegistry returns a Registry populated with the embedded
// built-in flows. Each entry is stamped with Builtin=true so callers
// (future: UI, API) can distinguish shipped flows from user ones
// without scanning the list.
func NewBuiltinRegistry() *Registry {
	reg := &Registry{byKey: make(map[string]Flow, len(builtins))}
	for _, f := range builtins {
		f.Builtin = true
		reg.order = append(reg.order, f.Slug)
		reg.byKey[f.Slug] = f
	}
	return reg
}

// NewRegistry returns a Registry populated with the given flows in
// declaration order. Exported so tests (and future user-authored
// loaders) can assemble custom registries without mutating package
// state. Builtin is left at each flow's supplied value — callers
// that want to distinguish user flows from built-ins set it
// themselves.
func NewRegistry(flows ...Flow) *Registry {
	reg := &Registry{byKey: make(map[string]Flow, len(flows))}
	for _, f := range flows {
		reg.order = append(reg.order, f.Slug)
		reg.byKey[f.Slug] = f
	}
	return reg
}

// Get returns the Flow with the given slug and whether it was found.
// The returned Flow is a deep copy so the caller cannot mutate
// registry state by assigning to Steps or RunInParallelWith.
func (r *Registry) Get(slug string) (Flow, bool) {
	f, ok := r.byKey[slug]
	if !ok {
		return Flow{}, false
	}
	return cloneFlow(f), true
}

// List returns every flow in registration order. The returned slice
// and every Flow within it are deep copies — mutating them does not
// leak back into the registry.
func (r *Registry) List() []Flow {
	out := make([]Flow, 0, len(r.order))
	for _, slug := range r.order {
		out = append(out, cloneFlow(r.byKey[slug]))
	}
	return out
}

// resolveExplicit is the shared body of ResolveForTask and
// ResolveRoutineFlow: prefer the explicit slug field, but only when it still
// names a registered flow; otherwise default to "implement". The old
// legacy-Kind mapping is gone -- every removed kind (idea-agent, the retired
// brainstorm / test-only flows) already resolved to "implement", so the kind
// extractor and ResolveLegacyKind were dead. explicitFlow differs per caller
// (FlowID vs RoutineSpawnFlow).
func (r *Registry) resolveExplicit(t *store.Task, explicitFlow func(*store.Task) string) string {
	if t == nil {
		return "implement"
	}
	// A task or routine pinned to a since-removed slug must keep dispatching
	// rather than resolve to a slug that no longer exists; fall back to the
	// default flow. User-authored flows are registered, so they resolve to
	// themselves.
	if s := explicitFlow(t); s != "" {
		if _, ok := r.byKey[s]; ok {
			return s
		}
	}
	return "implement"
}

// ResolveForTask returns the slug of the flow a task should run against: the
// task's explicit FlowID when it names a registered flow, otherwise "implement".
// This helper lives on the flow Registry (rather than as a *Task method) because
// the store package cannot import flow without creating a cycle.
func (r *Registry) ResolveForTask(t *store.Task) string {
	return r.resolveExplicit(t, func(t *store.Task) string { return t.FlowID })
}

// ResolveRoutineFlow returns the slug of the flow a routine spawns instance
// tasks against: RoutineSpawnFlow when registered, otherwise "implement".
func (r *Registry) ResolveRoutineFlow(t *store.Task) string {
	return r.resolveExplicit(t, func(t *store.Task) string { return t.RoutineSpawnFlow })
}

// cloneFlow produces a defensive deep copy of a Flow. Used by Get and
// List so callers can mutate the returned value without affecting the
// registry.
func cloneFlow(f Flow) Flow {
	out := f
	if len(f.Steps) > 0 {
		steps := make([]Step, len(f.Steps))
		for i, s := range f.Steps {
			steps[i] = s
			if len(s.RunInParallelWith) > 0 {
				parallel := make([]string, len(s.RunInParallelWith))
				copy(parallel, s.RunInParallelWith)
				steps[i].RunInParallelWith = parallel
			}
		}
		out.Steps = steps
	}
	return out
}
