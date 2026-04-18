package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"changkun.de/x/wallfacer/internal/agents"
)

// TestListAgents_ReturnsBuiltins asserts every built-in role shows up
// with a non-empty slug and title, and that heavy-weight agents declare
// the workspace.write capability so UI consumers can warn users.
func TestListAgents_ReturnsBuiltins(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	w := httptest.NewRecorder()
	h.ListAgents(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got []AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != len(agents.BuiltinAgents) {
		t.Fatalf("len = %d, want %d", len(got), len(agents.BuiltinAgents))
	}

	bySlug := map[string]AgentResponse{}
	for _, a := range got {
		if a.Slug == "" {
			t.Errorf("empty slug in response")
		}
		if a.Title == "" {
			t.Errorf("%s: empty title", a.Slug)
		}
		if !a.Builtin {
			t.Errorf("%s: expected builtin=true", a.Slug)
		}
		if a.PromptTmpl != "" {
			t.Errorf("%s: ListAgents must omit prompt_tmpl", a.Slug)
		}
		bySlug[a.Slug] = a
	}
	for _, slug := range []string{"impl", "test"} {
		row, ok := bySlug[slug]
		if !ok {
			t.Fatalf("%s missing from list", slug)
		}
		hasWrite := false
		for _, c := range row.Capabilities {
			if c == agents.CapWorkspaceWrite {
				hasWrite = true
			}
		}
		if !hasWrite {
			t.Errorf("%s: expected %q in capabilities, got %v",
				slug, agents.CapWorkspaceWrite, row.Capabilities)
		}
	}
}

// TestGetAgent_ReturnsPromptBody checks that the single-agent endpoint
// populates the prompt template body for a role that owns one.
func TestGetAgent_ReturnsPromptBody(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/title", nil)
	req.SetPathValue("slug", "title")
	w := httptest.NewRecorder()
	h.GetAgent(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var got AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Slug != "title" {
		t.Errorf("slug = %q, want title", got.Slug)
	}
	if got.PromptTmpl == "" {
		t.Error("prompt_tmpl is empty; expected the rendered title template")
	}
}

// TestGetAgent_UnknownReturns404 guards the 404 path.
func TestGetAgent_UnknownReturns404(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/does-not-exist", nil)
	req.SetPathValue("slug", "does-not-exist")
	w := httptest.NewRecorder()
	h.GetAgent(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// TestGetAgent_ImplementationHasNoPromptBody confirms roles without a
// standalone template (implementation) return an empty prompt_tmpl.
func TestGetAgent_ImplementationHasNoPromptBody(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/impl", nil)
	req.SetPathValue("slug", "impl")
	w := httptest.NewRecorder()
	h.GetAgent(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.PromptTmpl != "" {
		t.Errorf("prompt_tmpl = %q, want empty", got.PromptTmpl)
	}
}
