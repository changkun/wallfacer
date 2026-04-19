package handler

import (
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
