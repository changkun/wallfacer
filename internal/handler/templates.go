package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"changkun.de/x/wallfacer/internal/pkg/atomicfile"
	"github.com/google/uuid"
)

// PromptTemplate is a named reusable prompt fragment.
type PromptTemplate struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// templatesMu protects all reads and writes to the templates.json file.
var templatesMu sync.RWMutex

// templatesPath returns the filesystem path to the templates.json file.
func (h *Handler) templatesPath() string {
	return filepath.Join(h.configDir, "templates.json")
}

// loadTemplates reads and parses templates.json. Returns an empty slice
// (not an error) when the file does not exist.
func (h *Handler) loadTemplates() ([]PromptTemplate, error) {
	data, err := os.ReadFile(h.templatesPath())
	if errors.Is(err, os.ErrNotExist) {
		return []PromptTemplate{}, nil
	}
	if err != nil {
		return nil, err
	}
	var templates []PromptTemplate
	if err := json.Unmarshal(data, &templates); err != nil {
		return nil, err
	}
	return templates, nil
}

// saveTemplates writes the templates slice to templates.json atomically.
func (h *Handler) saveTemplates(templates []PromptTemplate) error {
	return atomicfile.WriteJSON(h.templatesPath(), templates, 0644)
}

// ListTemplates handles GET /api/templates.
// Returns all templates sorted by created_at descending; empty array when file absent.
func (h *Handler) ListTemplates(w http.ResponseWriter, _ *http.Request) {
	templatesMu.RLock()
	templates, err := h.loadTemplates()
	templatesMu.RUnlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slices.SortFunc(templates, func(a, b PromptTemplate) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})
	writeJSON(w, http.StatusOK, templates)
}

// CreateTemplate handles POST /api/templates.
// Expects JSON body {name, body}; returns 201 with the created template.
func (h *Handler) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Body string `json:"body"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if req.Name == "" || req.Body == "" {
		http.Error(w, "name and body are required", http.StatusBadRequest)
		return
	}
	tmpl := PromptTemplate{
		ID:        uuid.New().String(),
		Name:      req.Name,
		Body:      req.Body,
		CreatedAt: time.Now().UTC(),
	}

	templatesMu.Lock()
	defer templatesMu.Unlock()

	templates, err := h.loadTemplates()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	templates = append(templates, tmpl)
	if err := h.saveTemplates(templates); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, tmpl)
}

// DeleteTemplate handles DELETE /api/templates/{id}.
// Returns 404 if not found, 204 on success.
func (h *Handler) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	templatesMu.Lock()
	defer templatesMu.Unlock()

	templates, err := h.loadTemplates()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	idx := slices.IndexFunc(templates, func(t PromptTemplate) bool { return t.ID == id })
	if idx == -1 {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	}

	templates = append(templates[:idx], templates[idx+1:]...)
	if err := h.saveTemplates(templates); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
