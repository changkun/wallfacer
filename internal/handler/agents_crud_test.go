package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func postAgentJSON(t *testing.T, h *Handler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/agents", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.CreateAgent(rec, req)
	return rec
}

func TestCreateAgent_WritesUserAuthoredAgent(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	rec := postAgentJSON(t, h, map[string]any{
		"slug":    "impl-codex",
		"title":   "Implementation (Codex)",
		"harness": "codex",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	// The merged registry now knows the new slug.
	if _, ok := h.agentsRegistry().Get("impl-codex"); !ok {
		t.Error("agent not present in registry after create")
	}
}

func TestCreateAgent_RejectsBuiltinShadow(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	rec := postAgentJSON(t, h, map[string]any{
		"slug":  "impl",
		"title": "Shadow",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateAgent_RejectsInvalidHarness(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	rec := postAgentJSON(t, h, map[string]any{
		"slug":    "bad-harness",
		"title":   "Bad",
		"harness": "gpt",
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateAgent_BuiltinReturns409(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	body, _ := json.Marshal(map[string]any{"title": "tampered"})
	req := httptest.NewRequest(http.MethodPut, "/api/agents/impl", bytes.NewReader(body))
	req.SetPathValue("slug", "impl")
	rec := httptest.NewRecorder()
	h.UpdateAgent(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

func TestDeleteAgent_BuiltinReturns409(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/agents/impl", nil)
	req.SetPathValue("slug", "impl")
	rec := httptest.NewRecorder()
	h.DeleteAgent(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

func TestDeleteAgent_UserAuthoredRoundTrip(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	// Seed
	rec := postAgentJSON(t, h, map[string]any{"slug": "tmp-agent", "title": "Tmp"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed failed: %d; body=%s", rec.Code, rec.Body.String())
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/agents/tmp-agent", nil)
	req.SetPathValue("slug", "tmp-agent")
	rec = httptest.NewRecorder()
	h.DeleteAgent(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d, want 204", rec.Code)
	}
	if _, ok := h.agentsRegistry().Get("tmp-agent"); ok {
		t.Error("agent still present in registry after delete")
	}
}
