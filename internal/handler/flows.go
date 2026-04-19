package handler

import (
	"net/http"
	"sync"

	"changkun.de/x/wallfacer/internal/agents"
	"changkun.de/x/wallfacer/internal/flow"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
)

// flowRegistry returns the merged built-in + user-authored flow
// registry. Prefers the runner's registry when a runner is wired so
// the handler sees exactly the same catalog the dispatcher executes
// against. Tests without a runner fall back to a built-in-only
// singleton.
var (
	flowRegOnce sync.Once
	flowReg     *flow.Registry
)

func (h *Handler) flowsRegistry() *flow.Registry {
	if h.runner != nil {
		if reg := h.runner.FlowsRegistry(); reg != nil {
			return reg
		}
	}
	return flowRegistry()
}

func flowRegistry() *flow.Registry {
	flowRegOnce.Do(func() {
		flowReg = flow.NewBuiltinRegistry()
	})
	return flowReg
}

// StepResponse is the wire shape for a single step in a Flow. Fields
// match flow.Step with the addition of AgentName, which the handler
// resolves from the agents registry so the UI doesn't need a second
// round-trip per step.
type StepResponse struct {
	AgentSlug         string   `json:"agent_slug"`
	AgentName         string   `json:"agent_name,omitempty"`
	Optional          bool     `json:"optional,omitempty"`
	InputFrom         string   `json:"input_from,omitempty"`
	RunInParallelWith []string `json:"run_in_parallel_with,omitempty"`
}

// FlowResponse is the wire shape for a Flow surfaced on the Flows tab.
// SpawnKind is serialised as a string so the zero value collapses to
// omitempty for normal flows.
type FlowResponse struct {
	Slug        string         `json:"slug"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	SpawnKind   string         `json:"spawn_kind,omitempty"`
	Builtin     bool           `json:"builtin"`
	Steps       []StepResponse `json:"steps"`
}

func agentNameForSlug(slug string) string {
	for i := range agents.BuiltinAgents {
		if agents.BuiltinAgents[i].Slug == slug {
			return agents.BuiltinAgents[i].Title
		}
	}
	return ""
}

func describeFlow(f flow.Flow) FlowResponse {
	steps := make([]StepResponse, 0, len(f.Steps))
	for _, s := range f.Steps {
		steps = append(steps, StepResponse{
			AgentSlug:         s.AgentSlug,
			AgentName:         agentNameForSlug(s.AgentSlug),
			Optional:          s.Optional,
			InputFrom:         s.InputFrom,
			RunInParallelWith: s.RunInParallelWith,
		})
	}
	return FlowResponse{
		Slug:        f.Slug,
		Name:        f.Name,
		Description: f.Description,
		SpawnKind:   string(f.SpawnKind),
		Builtin:     f.Builtin,
		Steps:       steps,
	}
}

// ListFlows returns the merged flow catalog in registration order.
func (h *Handler) ListFlows(w http.ResponseWriter, _ *http.Request) {
	flows := h.flowsRegistry().List()
	out := make([]FlowResponse, 0, len(flows))
	for _, f := range flows {
		out = append(out, describeFlow(f))
	}
	httpjson.Write(w, http.StatusOK, out)
}

// GetFlow returns one flow by slug. 404 when the slug is unknown.
func (h *Handler) GetFlow(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	f, ok := h.flowsRegistry().Get(slug)
	if !ok {
		http.Error(w, "unknown flow: "+slug, http.StatusNotFound)
		return
	}
	httpjson.Write(w, http.StatusOK, describeFlow(f))
}
