package handler

import (
	"errors"
	"fmt"
	"net/http"
	"sync"

	"changkun.de/x/wallfacer/internal/agents"
	"changkun.de/x/wallfacer/internal/flow"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
	"changkun.de/x/wallfacer/internal/store"
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

// flowWriteRequest is the body shape for POST/PUT /api/flows. Uses
// the same field names as FlowResponse so UI forms can round-trip
// without renaming.
type flowWriteRequest struct {
	Slug        string               `json:"slug"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	SpawnKind   string               `json:"spawn_kind"`
	Steps       []flowStepWriteInput `json:"steps"`
}

type flowStepWriteInput struct {
	AgentSlug         string   `json:"agent_slug"`
	Optional          bool     `json:"optional"`
	InputFrom         string   `json:"input_from"`
	RunInParallelWith []string `json:"run_in_parallel_with"`
}

func (req flowWriteRequest) toFlow() flow.Flow {
	steps := make([]flow.Step, len(req.Steps))
	for i, s := range req.Steps {
		steps[i] = flow.Step{
			AgentSlug:         s.AgentSlug,
			Optional:          s.Optional,
			InputFrom:         s.InputFrom,
			RunInParallelWith: s.RunInParallelWith,
		}
	}
	return flow.Flow{
		Slug:        req.Slug,
		Name:        req.Name,
		Description: req.Description,
		SpawnKind:   store.TaskKind(req.SpawnKind),
		Steps:       steps,
	}
}

func (h *Handler) validateFlowWrite(req flowWriteRequest) error {
	if !flow.IsValidSlug(req.Slug) {
		return fmt.Errorf("slug %q is not kebab-case (2-40 chars, lowercase, digits, hyphens)", req.Slug)
	}
	if req.Name == "" {
		return errors.New("name is required")
	}
	if len(req.Steps) == 0 {
		return errors.New("flow must have at least one step")
	}
	aReg := h.agentsRegistry()
	seen := make(map[string]int, len(req.Steps))
	for i, s := range req.Steps {
		if s.AgentSlug == "" {
			return fmt.Errorf("step %d: agent_slug is required", i)
		}
		if _, ok := aReg.Get(s.AgentSlug); !ok {
			return fmt.Errorf("step %d: agent %q is not registered", i, s.AgentSlug)
		}
		seen[s.AgentSlug] = i
	}
	// Parallel-sibling references must resolve to a sibling in the
	// same flow, and a step must not list itself.
	for i, s := range req.Steps {
		for _, peer := range s.RunInParallelWith {
			if peer == s.AgentSlug {
				return fmt.Errorf("step %d: run_in_parallel_with cannot reference itself (%q)", i, peer)
			}
			if _, ok := seen[peer]; !ok {
				return fmt.Errorf("step %d: run_in_parallel_with %q is not a sibling step", i, peer)
			}
		}
	}
	return nil
}

// CreateFlow handles POST /api/flows.
func (h *Handler) CreateFlow(w http.ResponseWriter, r *http.Request) {
	req, ok := httpjson.DecodeBody[flowWriteRequest](w, r)
	if !ok {
		return
	}
	if err := h.validateFlowWrite(*req); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if flow.IsBuiltin(req.Slug) {
		http.Error(w, fmt.Sprintf("slug %q is a built-in; pick a different slug", req.Slug), http.StatusConflict)
		return
	}
	if _, dup := h.flowsRegistry().Get(req.Slug); dup {
		http.Error(w, fmt.Sprintf("slug %q already exists", req.Slug), http.StatusConflict)
		return
	}
	dir := h.runner.FlowsDir()
	if dir == "" {
		http.Error(w, "flows directory not configured", http.StatusServiceUnavailable)
		return
	}
	f := req.toFlow()
	if err := flow.WriteUserFlow(dir, f); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.runner.ReloadFlows(); err != nil {
		http.Error(w, "wrote flow but reload failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	httpjson.Write(w, http.StatusCreated, describeFlow(f))
}

// UpdateFlow handles PUT /api/flows/{slug}. Built-in flows are
// read-only.
func (h *Handler) UpdateFlow(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if flow.IsBuiltin(slug) {
		http.Error(w, fmt.Sprintf("flow %q is built-in and read-only; clone it first", slug), http.StatusConflict)
		return
	}
	if _, exists := h.flowsRegistry().Get(slug); !exists {
		http.Error(w, "unknown flow: "+slug, http.StatusNotFound)
		return
	}
	req, ok := httpjson.DecodeBody[flowWriteRequest](w, r)
	if !ok {
		return
	}
	req.Slug = slug
	if err := h.validateFlowWrite(*req); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	dir := h.runner.FlowsDir()
	if dir == "" {
		http.Error(w, "flows directory not configured", http.StatusServiceUnavailable)
		return
	}
	f := req.toFlow()
	if err := flow.WriteUserFlow(dir, f); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.runner.ReloadFlows(); err != nil {
		http.Error(w, "wrote flow but reload failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	httpjson.Write(w, http.StatusOK, describeFlow(f))
}

// DeleteFlow handles DELETE /api/flows/{slug}.
func (h *Handler) DeleteFlow(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if flow.IsBuiltin(slug) {
		http.Error(w, fmt.Sprintf("flow %q is built-in and cannot be deleted", slug), http.StatusConflict)
		return
	}
	dir := h.runner.FlowsDir()
	if dir == "" {
		http.Error(w, "flows directory not configured", http.StatusServiceUnavailable)
		return
	}
	if err := flow.DeleteUserFlow(dir, slug); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.runner.ReloadFlows(); err != nil {
		http.Error(w, "deleted flow but reload failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
