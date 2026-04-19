package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func postFlowJSON(t *testing.T, h *Handler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/flows", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.CreateFlow(rec, req)
	return rec
}

func TestCreateFlow_WritesUserAuthoredFlow(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	rec := postFlowJSON(t, h, map[string]any{
		"slug": "tdd-loop",
		"name": "TDD Loop",
		"steps": []map[string]any{
			{"agent_slug": "test"},
			{"agent_slug": "impl", "input_from": "test"},
		},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := h.flowsRegistry().Get("tdd-loop"); !ok {
		t.Error("flow not present in registry after create")
	}
}

func TestCreateFlow_RejectsBuiltinShadow(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	rec := postFlowJSON(t, h, map[string]any{
		"slug":  "implement",
		"name":  "Shadow",
		"steps": []map[string]any{{"agent_slug": "impl"}},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateFlow_RejectsUnknownAgentSlug(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	rec := postFlowJSON(t, h, map[string]any{
		"slug":  "bad",
		"name":  "Bad",
		"steps": []map[string]any{{"agent_slug": "no-such-agent"}},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateFlow_RejectsSelfParallel(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	rec := postFlowJSON(t, h, map[string]any{
		"slug": "self-par",
		"name": "Self",
		"steps": []map[string]any{
			{"agent_slug": "impl", "run_in_parallel_with": []string{"impl"}},
		},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateFlow_BuiltinReturns409(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	body, _ := json.Marshal(map[string]any{
		"name":  "Tamper",
		"steps": []map[string]any{{"agent_slug": "impl"}},
	})
	req := httptest.NewRequest(http.MethodPut, "/api/flows/implement", bytes.NewReader(body))
	req.SetPathValue("slug", "implement")
	rec := httptest.NewRecorder()
	h.UpdateFlow(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

func TestDeleteFlow_UserAuthoredRoundTrip(t *testing.T) {
	h, _ := newTestHandlerWithPrompts(t)
	rec := postFlowJSON(t, h, map[string]any{
		"slug":  "tmp-flow",
		"name":  "Tmp",
		"steps": []map[string]any{{"agent_slug": "impl"}},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed failed: %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/flows/tmp-flow", nil)
	req.SetPathValue("slug", "tmp-flow")
	rec = httptest.NewRecorder()
	h.DeleteFlow(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d, want 204", rec.Code)
	}
	if _, ok := h.flowsRegistry().Get("tmp-flow"); ok {
		t.Error("flow still present after delete")
	}
}
