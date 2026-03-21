package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestListTemplates_MissingFile returns an empty JSON array and status 200
// when templates.json does not exist.
func TestListTemplates_MissingFile(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/templates", nil)
	w := httptest.NewRecorder()
	h.ListTemplates(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var templates []PromptTemplate
	if err := json.NewDecoder(w.Body).Decode(&templates); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(templates) != 0 {
		t.Errorf("expected empty array, got %d items", len(templates))
	}
}

// TestCreateTemplate_MissingName returns 400 when name is empty.
func TestCreateTemplate_MissingName(t *testing.T) {
	h := newTestHandler(t)

	body := `{"name":"","body":"some body"}`
	req := httptest.NewRequest(http.MethodPost, "/api/templates", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.CreateTemplate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TestCreateTemplate_MissingBody returns 400 when body is empty.
func TestCreateTemplate_MissingBody(t *testing.T) {
	h := newTestHandler(t)

	body := `{"name":"My Template","body":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/templates", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.CreateTemplate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TestCreateTemplate_MissingBoth returns 400 when both name and body are empty.
func TestCreateTemplate_MissingBoth(t *testing.T) {
	h := newTestHandler(t)

	body := `{"name":"","body":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/templates", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.CreateTemplate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TestCreateTemplate_InvalidJSON returns 400 for malformed request body.
func TestCreateTemplate_InvalidJSON(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/templates", strings.NewReader("{bad json"))
	w := httptest.NewRecorder()
	h.CreateTemplate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TestTemplates_RoundTrip creates a template, lists it, and verifies the fields.
func TestTemplates_RoundTrip(t *testing.T) {
	h := newTestHandler(t)

	// Create.
	createBody := `{"name":"Fix bug","body":"Please fix the following bug:\n\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/templates", strings.NewReader(createBody))
	w := httptest.NewRecorder()
	h.CreateTemplate(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created PromptTemplate
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if created.ID == "" {
		t.Error("expected non-empty ID")
	}
	if created.Name != "Fix bug" {
		t.Errorf("expected name 'Fix bug', got %q", created.Name)
	}
	if created.Body != "Please fix the following bug:\n\n" {
		t.Errorf("unexpected body: %q", created.Body)
	}
	if created.CreatedAt.IsZero() {
		t.Error("expected non-zero created_at")
	}

	// List.
	req2 := httptest.NewRequest(http.MethodGet, "/api/templates", nil)
	w2 := httptest.NewRecorder()
	h.ListTemplates(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w2.Code)
	}
	var templates []PromptTemplate
	if err := json.NewDecoder(w2.Body).Decode(&templates); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(templates) != 1 {
		t.Fatalf("expected 1 template, got %d", len(templates))
	}
	if templates[0].ID != created.ID {
		t.Errorf("expected id %q, got %q", created.ID, templates[0].ID)
	}
	if templates[0].Name != created.Name {
		t.Errorf("expected name %q, got %q", created.Name, templates[0].Name)
	}
}

// TestTemplates_Delete creates a template, deletes it, then verifies it is absent.
func TestTemplates_Delete(t *testing.T) {
	h := newTestHandler(t)

	// Create.
	createBody := `{"name":"Template A","body":"Body A"}`
	req := httptest.NewRequest(http.MethodPost, "/api/templates", strings.NewReader(createBody))
	w := httptest.NewRecorder()
	h.CreateTemplate(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created PromptTemplate
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}

	// Delete.
	delReq := httptest.NewRequest(http.MethodDelete, "/api/templates/"+created.ID, nil)
	delReq.SetPathValue("id", created.ID)
	delW := httptest.NewRecorder()
	h.DeleteTemplate(delW, delReq)

	if delW.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d: %s", delW.Code, delW.Body.String())
	}

	// List: should be empty.
	listReq := httptest.NewRequest(http.MethodGet, "/api/templates", nil)
	listW := httptest.NewRecorder()
	h.ListTemplates(listW, listReq)

	var templates []PromptTemplate
	if err := json.NewDecoder(listW.Body).Decode(&templates); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(templates) != 0 {
		t.Errorf("expected empty list after delete, got %d items", len(templates))
	}
}

// TestTemplates_DeleteUnknown returns 404 for an unknown template ID.
func TestTemplates_DeleteUnknown(t *testing.T) {
	h := newTestHandler(t)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/templates/nonexistent-id", nil)
	delReq.SetPathValue("id", "nonexistent-id")
	w := httptest.NewRecorder()
	h.DeleteTemplate(w, delReq)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// TestListTemplates_SortedByCreatedAtDesc verifies that multiple templates are
// returned newest-first.
func TestListTemplates_SortedByCreatedAtDesc(t *testing.T) {
	h := newTestHandler(t)

	names := []string{"Alpha", "Beta", "Gamma"}
	for _, name := range names {
		body := `{"name":"` + name + `","body":"body of ` + name + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/templates", strings.NewReader(body))
		w := httptest.NewRecorder()
		h.CreateTemplate(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: expected 201, got %d", name, w.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/templates", nil)
	w := httptest.NewRecorder()
	h.ListTemplates(w, req)

	var templates []PromptTemplate
	if err := json.NewDecoder(w.Body).Decode(&templates); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(templates) != 3 {
		t.Fatalf("expected 3 templates, got %d", len(templates))
	}
	// Newest should be first (Gamma was created last).
	if templates[0].Name != "Gamma" {
		t.Errorf("expected Gamma first (newest), got %q", templates[0].Name)
	}
}
