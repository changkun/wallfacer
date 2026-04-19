package handler

import (
	"errors"
	"fmt"
	"net/http"

	"changkun.de/x/wallfacer/internal/agents"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
)

// AgentResponse is the wire shape for an agent descriptor surfaced on
// the Agents tab. The fields mirror the neutral descriptor in
// internal/agents; runner-side dispatch plumbing (mount profile, parse
// function, sandbox-routing activity) is intentionally NOT exposed
// because it is orchestration detail that a Flow composer should not
// need to know.
//
// Design note: an earlier version of this endpoint returned
// `mount_mode` / `single_turn` / `activity`. Those were runner plumbing
// leaking through the wire and were replaced by `capabilities` (a
// stable vocabulary of what the agent needs) and `multiturn` (advisory
// metadata). Clients that rendered mount_mode should read capabilities
// instead.
type AgentResponse struct {
	Slug               string   `json:"slug"`
	Title              string   `json:"title"`
	Description        string   `json:"description,omitempty"`
	Capabilities       []string `json:"capabilities,omitempty"`
	Multiturn          bool     `json:"multiturn"`
	Harness            string   `json:"harness,omitempty"`
	PromptTemplateName string   `json:"prompt_template_name,omitempty"`
	Builtin            bool     `json:"builtin"`
	PromptTmpl         string   `json:"prompt_tmpl,omitempty"` // only populated by GetAgent
}

func describeAgent(role agents.Role) AgentResponse {
	return AgentResponse{
		Slug:               role.Slug,
		Title:              role.Title,
		Description:        role.Description,
		Capabilities:       role.Capabilities,
		Multiturn:          role.Multiturn,
		Harness:            role.Harness,
		PromptTemplateName: role.PromptTemplateName,
		Builtin:            agents.IsBuiltin(role.Slug),
	}
}

// agentsRegistry returns the merged built-in + user-authored
// registry. Falls back to a built-in-only registry when the runner
// is not wired (test harnesses).
func (h *Handler) agentsRegistry() *agents.Registry {
	if h.runner != nil {
		if reg := h.runner.AgentsRegistry(); reg != nil {
			return reg
		}
	}
	return agents.NewBuiltinRegistry()
}

// ListAgents returns the merged agent catalog in registration order.
// The prompt template body is intentionally omitted; clients fetch
// it per-agent via GetAgent to keep the list payload small.
func (h *Handler) ListAgents(w http.ResponseWriter, _ *http.Request) {
	roles := h.agentsRegistry().List()
	out := make([]AgentResponse, 0, len(roles))
	for _, role := range roles {
		out = append(out, describeAgent(role))
	}
	httpjson.Write(w, http.StatusOK, out)
}

// GetAgent returns the full descriptor including the prompt template
// body for a single agent. 404 when the slug is unknown.
func (h *Handler) GetAgent(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	role, ok := h.agentsRegistry().Get(slug)
	if !ok {
		http.Error(w, "unknown agent: "+slug, http.StatusNotFound)
		return
	}

	resp := describeAgent(role)
	if role.PromptTemplateName != "" {
		content, _, err := h.runner.Prompts().Content(role.PromptTemplateName)
		if err == nil {
			resp.PromptTmpl = content
		}
	}
	httpjson.Write(w, http.StatusOK, resp)
}

// agentWriteRequest is the body shape for POST/PUT /api/agents. Only
// the fields a user can safely edit are exposed; runner-private
// binding info (mount profile, parse function) stays built-in.
type agentWriteRequest struct {
	Slug               string   `json:"slug"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	PromptTemplateName string   `json:"prompt_template_name"`
	Capabilities       []string `json:"capabilities"`
	Multiturn          bool     `json:"multiturn"`
	Harness            string   `json:"harness"`
}

func (req agentWriteRequest) toRole() agents.Role {
	return agents.Role{
		Slug:               req.Slug,
		Title:              req.Title,
		Description:        req.Description,
		PromptTemplateName: req.PromptTemplateName,
		Capabilities:       req.Capabilities,
		Multiturn:          req.Multiturn,
		Harness:            req.Harness,
	}
}

func validateAgentWrite(req agentWriteRequest) error {
	if !agents.IsValidSlug(req.Slug) {
		return fmt.Errorf("slug %q is not kebab-case (2-40 chars, lowercase, digits, hyphens)", req.Slug)
	}
	if req.Title == "" {
		return errors.New("title is required")
	}
	switch req.Harness {
	case "", "claude", "codex":
	default:
		return fmt.Errorf("harness %q must be empty, claude, or codex", req.Harness)
	}
	return nil
}

// CreateAgent handles POST /api/agents. Writes the role to the
// user-authored agents directory and reloads the runner's merged
// registry so subsequent task launches see it immediately.
func (h *Handler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	req, ok := httpjson.DecodeBody[agentWriteRequest](w, r)
	if !ok {
		return
	}
	if err := validateAgentWrite(*req); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if agents.IsBuiltin(req.Slug) {
		http.Error(w, fmt.Sprintf("slug %q is a built-in; pick a different slug", req.Slug), http.StatusConflict)
		return
	}
	if _, dup := h.agentsRegistry().Get(req.Slug); dup {
		http.Error(w, fmt.Sprintf("slug %q already exists", req.Slug), http.StatusConflict)
		return
	}
	dir := h.runner.AgentsDir()
	if dir == "" {
		http.Error(w, "agents directory not configured", http.StatusServiceUnavailable)
		return
	}
	if err := agents.WriteUserAgent(dir, req.toRole()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.runner.ReloadAgents(); err != nil {
		http.Error(w, "wrote agent but reload failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	httpjson.Write(w, http.StatusCreated, describeAgent(req.toRole()))
}

// UpdateAgent handles PUT /api/agents/{slug}. Built-in agents are
// read-only — clients must clone to edit.
func (h *Handler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if agents.IsBuiltin(slug) {
		http.Error(w, fmt.Sprintf("agent %q is built-in and read-only; clone it first", slug), http.StatusConflict)
		return
	}
	if _, exists := h.agentsRegistry().Get(slug); !exists {
		http.Error(w, "unknown agent: "+slug, http.StatusNotFound)
		return
	}
	req, ok := httpjson.DecodeBody[agentWriteRequest](w, r)
	if !ok {
		return
	}
	// Path slug wins over the body slug to prevent rename-by-PUT.
	req.Slug = slug
	if err := validateAgentWrite(*req); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	dir := h.runner.AgentsDir()
	if dir == "" {
		http.Error(w, "agents directory not configured", http.StatusServiceUnavailable)
		return
	}
	if err := agents.WriteUserAgent(dir, req.toRole()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.runner.ReloadAgents(); err != nil {
		http.Error(w, "wrote agent but reload failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	httpjson.Write(w, http.StatusOK, describeAgent(req.toRole()))
}

// DeleteAgent handles DELETE /api/agents/{slug}. Built-in agents
// cannot be deleted; the response is idempotent for user-authored
// agents (204 even if the file was already gone).
func (h *Handler) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if agents.IsBuiltin(slug) {
		http.Error(w, fmt.Sprintf("agent %q is built-in and cannot be deleted", slug), http.StatusConflict)
		return
	}
	dir := h.runner.AgentsDir()
	if dir == "" {
		http.Error(w, "agents directory not configured", http.StatusServiceUnavailable)
		return
	}
	if err := agents.DeleteUserAgent(dir, slug); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.runner.ReloadAgents(); err != nil {
		http.Error(w, "deleted agent but reload failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
