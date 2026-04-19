package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestListFlows_ReturnsBuiltins asserts every built-in flow shows up
// with a non-empty slug/name, declares builtin=true, and that the
// "implement" flow includes the terminal parallel triple.
func TestListFlows_ReturnsBuiltins(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	req := httptest.NewRequest(http.MethodGet, "/api/flows", nil)
	w := httptest.NewRecorder()
	h.ListFlows(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var got []FlowResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	bySlug := map[string]FlowResponse{}
	for _, f := range got {
		if f.Slug == "" {
			t.Errorf("empty slug in response")
		}
		if f.Name == "" {
			t.Errorf("%s: empty name", f.Slug)
		}
		if !f.Builtin {
			t.Errorf("%s: expected builtin=true", f.Slug)
		}
		bySlug[f.Slug] = f
	}
	for _, slug := range []string{"implement", "brainstorm", "refine-only", "test-only"} {
		if _, ok := bySlug[slug]; !ok {
			t.Errorf("missing expected flow %q", slug)
		}
	}
	impl, ok := bySlug["implement"]
	if !ok {
		t.Fatal("implement flow missing")
	}
	// Confirm parallel triple surfaces in the step list.
	triple := map[string]bool{"commit-msg": false, "title": false, "oversight": false}
	for _, s := range impl.Steps {
		if _, ok := triple[s.AgentSlug]; ok {
			triple[s.AgentSlug] = len(s.RunInParallelWith) == 2
		}
	}
	for slug, ok := range triple {
		if !ok {
			t.Errorf("implement step %q missing expected run_in_parallel_with pair", slug)
		}
	}
}

// TestGetFlow_ResolvesAgentNames asserts every step in a fetched flow
// has its AgentName populated via the agents registry lookup.
func TestGetFlow_ResolvesAgentNames(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	req := httptest.NewRequest(http.MethodGet, "/api/flows/implement", nil)
	req.SetPathValue("slug", "implement")
	w := httptest.NewRecorder()
	h.GetFlow(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var got FlowResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Slug != "implement" {
		t.Fatalf("slug = %q, want implement", got.Slug)
	}
	if len(got.Steps) == 0 {
		t.Fatal("expected non-empty steps")
	}
	for _, s := range got.Steps {
		if s.AgentSlug == "" {
			t.Errorf("step has empty agent_slug")
		}
		if s.AgentName == "" {
			t.Errorf("step %q: agent_name unresolved", s.AgentSlug)
		}
	}
}

// TestGetFlow_UnknownReturns404 guards the 404 path.
func TestGetFlow_UnknownReturns404(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	req := httptest.NewRequest(http.MethodGet, "/api/flows/does-not-exist", nil)
	req.SetPathValue("slug", "does-not-exist")
	w := httptest.NewRecorder()
	h.GetFlow(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
