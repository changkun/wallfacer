package flow

import "changkun.de/x/wallfacer/internal/store"

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

// ResolveLegacyKind maps the legacy store.TaskKind values onto their
// equivalent flow slugs so callers can migrate incrementally. The
// mapping is:
//
//   - "" (default)   → "implement"
//   - "idea-agent"   → "brainstorm"
//
// Other kinds (including "planning", "routine") return ok=false —
// those tasks continue to use their existing dispatch paths until
// their own migration tasks land. Returning the Flow by value keeps
// callers isolated from mutation, same as Get.
func (r *Registry) ResolveLegacyKind(kind store.TaskKind) (Flow, bool) {
	switch kind {
	case "":
		return r.Get("implement")
	case store.TaskKindIdeaAgent:
		return r.Get("brainstorm")
	default:
		return Flow{}, false
	}
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
