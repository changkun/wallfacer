package handler

import (
	"net/http"

	"changkun.de/x/wallfacer/internal/agents"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
)

// AgentResponse is the JSON shape for a single sub-agent role surfaced
// on the Agents tab. ListAgents returns a slice of these; GetAgent adds
// the rendered prompt-template body.
type AgentResponse struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Activity    string `json:"activity"`
	MountMode   string `json:"mount_mode"`
	SingleTurn  bool   `json:"single_turn"`
	TimeoutSec  int    `json:"timeout_sec,omitempty"`
	Builtin     bool   `json:"builtin"`
	PromptTmpl  string `json:"prompt_tmpl,omitempty"` // only populated by GetAgent
}

// agentSlugToPromptAPI maps an agent's Name (kebab slug) to the
// prompts-package API name of the template it renders. The empty
// string means the role does not own a standalone template (e.g.
// implementation consumes the user's task prompt verbatim).
var agentSlugToPromptAPI = map[string]string{
	"title":      "title",
	"oversight":  "oversight",
	"commit-msg": "commit_message",
	"refine":     "refinement",
	"ideate":     "ideation",
	"impl":       "",
	"test":       "test_verification",
}

// describeAgent converts a descriptor to the wire shape without the
// prompt body. Callers that want the body call resolvePromptBody.
func describeAgent(role agents.Role) AgentResponse {
	resp := AgentResponse{
		Slug:        role.Name,
		Name:        role.Name,
		Description: role.Description,
		Activity:    string(role.Activity),
		MountMode:   role.MountMode.String(),
		SingleTurn:  role.SingleTurn,
		Builtin:     true,
	}
	if role.Timeout != nil {
		resp.TimeoutSec = int(role.Timeout(nil).Seconds())
	}
	return resp
}

// ListAgents returns the full built-in agent catalog in registration
// order. The prompt template body is intentionally omitted; clients
// fetch it per-agent via GetAgent to keep the list payload small.
func (h *Handler) ListAgents(w http.ResponseWriter, _ *http.Request) {
	roles := agents.BuiltinAgents
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
	var match *agents.Role
	for i := range agents.BuiltinAgents {
		if agents.BuiltinAgents[i].Name == slug {
			match = &agents.BuiltinAgents[i]
			break
		}
	}
	if match == nil {
		http.Error(w, "unknown agent: "+slug, http.StatusNotFound)
		return
	}

	resp := describeAgent(*match)
	if apiName := agentSlugToPromptAPI[slug]; apiName != "" {
		content, _, err := h.runner.Prompts().Content(apiName)
		if err == nil {
			resp.PromptTmpl = content
		}
	}
	httpjson.Write(w, http.StatusOK, resp)
}
